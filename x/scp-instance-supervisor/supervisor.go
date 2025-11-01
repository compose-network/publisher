package scpsupervisor

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/compose-network/publisher/x/superblock/queue"
	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/compose-network/specs/compose/scp"
	"github.com/rs/zerolog"
)

// ErrInstanceAlreadyActive indicates an SCP instance with the same InstanceID is active.
var ErrInstanceAlreadyActive = errors.New("instance-scpSupervisor: scp instance already active")

// ErrInstanceNotFound indicates that an SCP instance was not found.
var ErrInstanceNotFound = errors.New("instance-scpSupervisor: instance not found")

// scpSupervisor implements SCPSupervisor, managing the lifecycle of multiple SCP instances (start, votes, timeout, and finalization).
// Whenever an instance finalizes (or can't be properly started), the OnFinalize hook is invoked.
type scpSupervisor struct {
	mu     sync.RWMutex
	logger zerolog.Logger

	// Dependencies
	factory   SCPFactory
	messenger scp.PublisherNetwork

	// Time
	instanceTimeout time.Duration
	timerFactory    TimerFactory
	now             func() time.Time

	// State
	active  map[string]*ActiveInstance
	history []CompletedInstance

	// History cleanup
	maxHistory       int
	historyRetention time.Duration

	// Hooks
	OnFinalize OnFinalizeHook
}

// New creates a SCPSupervisor using the provided config.
// Required fields: Factory, Network.
func New(cfg Config) SCPSupervisor {
	return &scpSupervisor{
		mu:     sync.RWMutex{},
		logger: cfg.Logger,
		// Dependencies
		factory:   cfg.Factory,
		messenger: cfg.Network,
		// Time
		instanceTimeout: cfg.InstanceTimeout,
		timerFactory:    cfg.TimerFactory,
		now:             cfg.Now,
		// State
		active:  make(map[string]*ActiveInstance),
		history: make([]CompletedInstance, 0),
		// Hooks
		OnFinalize: cfg.OnFinalize,
		// Cleanup
		maxHistory:       cfg.MaxHistory,
		historyRetention: cfg.HistoryRetention,
	}
}

func (s *scpSupervisor) SetOnFinalizeHook(hook OnFinalizeHook) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.OnFinalize = hook
}

// StartInstance creates and runs a new SCP instance
func (s *scpSupervisor) StartInstance(ctx context.Context, queued *queue.QueuedXTRequest, instance compose.Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if instance already exists
	key := instance.ID.String()
	if _, exists := s.active[key]; exists {
		s.OnFinalize(instance) // best effort to free chains
		return ErrInstanceAlreadyActive
	}

	// Create SCP runner
	runner, err := s.factory(instance, s.messenger, s.logger.With().Str("component", "scp").Str("instance-id", key).Logger())
	if err != nil {
		s.OnFinalize(instance)
		return err
	}

	entry := &ActiveInstance{
		Key:        key,
		Instance:   cloneInstance(instance),
		Runner:     runner,
		EnqueuedAt: queued.SubmittedAt,
		StartedAt:  s.now(),
	}

	// Set timeout
	if s.instanceTimeout > 0 && s.timerFactory != nil {
		entry.Timer = s.timerFactory.AfterFunc(s.instanceTimeout, func() { s.handleTimeout(entry) })
	}

	s.active[key] = entry
	runner.Run()

	s.logger.Info().Str("instance_id", key).Uint64("period_id", uint64(instance.PeriodID)).Uint64("sequence", uint64(instance.SequenceNumber)).Msg("SCP instance started")
	return nil
}

// HandleVote routes a vote to the appropriate instance and checks for finalization.
func (s *scpSupervisor) HandleVote(ctx context.Context, vote *pb.Vote) error {
	s.mu.RLock()
	key := instanceIDString(vote.InstanceId)
	entry := s.active[key]
	s.mu.RUnlock()
	if entry == nil {
		return ErrInstanceNotFound
	}

	if err := entry.Runner.ProcessVote(compose.ChainID(vote.ChainId), vote.Vote); err != nil {
		return err
	}

	s.tryFinalize(ctx, entry, DecisionSourceMessage)
	return nil
}

// handleTimeout is invoked after an instance timeout.
// It calls the Timeout method and tries to finalize.
func (s *scpSupervisor) handleTimeout(entry *ActiveInstance) {
	ctx := context.Background()
	if err := entry.Runner.Timeout(); err != nil {
		s.logger.Error().Err(err).Str("instance_id", entry.Key).Msg("timeout callback failed")
		return
	}
	s.tryFinalize(ctx, entry, DecisionSourceTimeout)
}

// tryFinalize checks if the instance has finalized and, if so, performs cleanup and notification.
func (s *scpSupervisor) tryFinalize(ctx context.Context, entry *ActiveInstance, source DecisionSource) {
	state := entry.Runner.DecisionState()
	if state == compose.DecisionStatePending {
		return
	}
	entry.finalOnce.Do(func() {
		s.mu.Lock()
		if entry.Timer != nil {
			entry.Timer.Stop()
		}
		delete(s.active, entry.Key)

		decision := CompletedInstance{
			Instance:   cloneInstance(entry.Instance),
			Accepted:   state == compose.DecisionStateAccepted,
			Source:     source,
			EnqueuedAt: entry.EnqueuedAt,
			StartedAt:  entry.StartedAt,
			DecidedAt:  s.now(),
		}
		s.history = append(s.history, decision)
		s.pruneHistoryLocked()
		s.mu.Unlock()

		if s.OnFinalize != nil {
			s.OnFinalize(entry.Instance)
		}

		s.logger.Info().Str("instance_id", entry.Key).Bool("accepted", decision.Accepted).Str("source", string(source)).Msg("SCP instance finalized")
	})
}

// History returns a shallow copy of decisions.
func (s *scpSupervisor) History() []CompletedInstance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CompletedInstance, len(s.history))
	copy(out, s.history)
	return out
}

// Stop stops the scpSupervisor, best-effort finalizing active instances.
func (s *scpSupervisor) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.RLock()
	active := make([]*ActiveInstance, 0, len(s.active))
	for _, entry := range s.active {
		active = append(active, entry)
	}
	s.mu.RUnlock()

	for _, entry := range active {
		if entry.Timer != nil {
			entry.Timer.Stop()
		}
		if err := entry.Runner.Timeout(); err != nil {
			s.logger.Error().Err(err).Str("instance_id", entry.Key).Msg("failed to trigger timeout during stop")
		}
		s.tryFinalize(ctx, entry, DecisionSourceTimeout)
	}

	return nil
}

// pruneHistoryLocked prunes history both by max size and oldness.
// Caller must hold s.mu.
func (s *scpSupervisor) pruneHistoryLocked() {
	if len(s.history) == 0 {
		return
	}

	// Trim by size if configured
	if s.maxHistory > 0 && len(s.history) > s.maxHistory {
		s.history = s.history[len(s.history)-s.maxHistory:]
	}

	// Trim by retention window if configured
	if s.historyRetention > 0 {
		cutoffTime := s.now().Add(-s.historyRetention)
		idx := 0
		for idx < len(s.history) && !s.history[idx].DecidedAt.After(cutoffTime) {
			idx++
		}
		if idx == 0 {
			return
		}
		for i := 0; i < idx; i++ {
			s.history[i] = CompletedInstance{}
		}
		s.history = s.history[idx:]
	}
}
