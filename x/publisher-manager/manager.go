package publishermanager

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/compose-network/publisher/x/messenger"
	periodrunner "github.com/compose-network/publisher/x/period-runner"
	sbcpcontroller "github.com/compose-network/publisher/x/sbcp-controller"
	scpsupervisor "github.com/compose-network/publisher/x/scp-instance-supervisor"
	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/rs/zerolog"
)

type publisherManager struct {
	// Synchronization and lifecycle
	mu        sync.RWMutex
	ctx       context.Context
	notifyCtx context.Context
	cancel    context.CancelFunc
	started   bool
	logger    zerolog.Logger

	// Modules
	messenger      messenger.Messenger
	sbcpController sbcpcontroller.SBCPController
	scpSupervisor  scpsupervisor.SCPSupervisor
	periodRunner   periodrunner.PeriodRunner
}

func New(cfg Config) (PublisherManager, error) {
	if err := cfg.apply(); err != nil {
		return nil, err
	}

	msgr := messenger.NewMessenger(
		cfg.Context,
		cfg.Logger.With().Str("component", "messenger").Logger(),
		cfg.Broadcaster,
	)

	periodRunner := periodrunner.NewLocalPeriodRunner(
		periodrunner.DefaultPeriodRunnerConfig(cfg.Logger.With().Str("component", "period-runner").Logger()),
	)
	currentPeriodID, _ := periodRunner.PeriodForTime(time.Now())

	scpSupervisor := scpsupervisor.New(scpsupervisor.DefaultConfig(
		cfg.Logger.With().Str("component", "scp-instance-scpSupervisor").Logger(),
		msgr))

	sbcpCtrl, err := sbcpcontroller.New(sbcpcontroller.DefaultConfig(
		cfg.Logger.With().Str("component", "sbcp-sbcpController").Logger(),
		msgr,
		compose.PeriodID(currentPeriodID-1), // Check sbcp SBCPController for explanation
		0,
		compose.SuperBlockHash{},
		0,
	))
	if err != nil {
		return nil, fmt.Errorf("publisherManager: create sbcp sbcpController: %w", err)
	}

	mgr := &publisherManager{
		ctx:            cfg.Context,
		notifyCtx:      cfg.Context,
		logger:         cfg.Logger,
		messenger:      msgr,
		sbcpController: sbcpCtrl,
		scpSupervisor:  scpSupervisor,
		periodRunner:   periodRunner,
	}

	// Set hooks
	periodRunner.SetHandler(func(ctx context.Context, _ periodrunner.PeriodInfo) error {
		return mgr.sbcpController.OnNewPeriod(ctx)
	})
	sbcpCtrl.SetInstanceStarter(scpSupervisor)
	mgr.scpSupervisor.SetOnFinalizeHook(func(instance compose.Instance) {
		ctx := mgr.notificationContext()
		if err := mgr.sbcpController.NotifyInstanceDecided(ctx, instance); err != nil {
			mgr.logger.Error().Err(err).Msg("failed to notify sbcp about instance decision")
		}
	})

	return mgr, nil
}

func (m *publisherManager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	m.ctx = runCtx
	m.notifyCtx = runCtx
	m.cancel = cancel
	m.started = true
	m.mu.Unlock()

	if err := m.periodRunner.Start(runCtx); err != nil {
		cancel()
		m.mu.Lock()
		m.started = false
		m.cancel = nil
		m.ctx = nil
		m.notifyCtx = nil
		m.mu.Unlock()
		return fmt.Errorf("publisherManager: start period runner: %w", err)
	}

	return nil
}

func (m *publisherManager) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	cancel := m.cancel
	m.started = false
	m.cancel = nil
	m.ctx = nil
	m.notifyCtx = ctx
	m.mu.Unlock()

	cancel()

	var errs []error
	if err := m.periodRunner.Stop(ctx); err != nil {
		errs = append(errs, fmt.Errorf("publisherManager: stop period runner: %w", err))
	}
	if err := m.scpSupervisor.Stop(ctx); err != nil {
		errs = append(errs, fmt.Errorf("publisherManager: stop scp supervisor: %w", err))
	}
	if err := m.sbcpController.Stop(ctx); err != nil {
		errs = append(errs, fmt.Errorf("publisherManager: stop sbcp controller: %w", err))
	}

	m.mu.Lock()
	m.notifyCtx = nil
	m.mu.Unlock()

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (m *publisherManager) HandleMessage(ctx context.Context, from string, msg *pb.Message) error {
	if msg == nil {
		return nil
	}
	switch payload := msg.Payload.(type) {
	case *pb.Message_XtRequest:
		if payload.XtRequest == nil {
			return nil
		}
		return m.sbcpController.EnqueueXTRequest(ctx, payload.XtRequest, from)
	case *pb.Message_Vote:
		if payload.Vote == nil {
			return nil
		}
		return m.scpSupervisor.HandleVote(ctx, payload.Vote)
	default:
		return nil
	}
}

func (m *publisherManager) QueueStats(ctx context.Context) (int, error) {
	return 0, nil
}

func (m *publisherManager) notificationContext() context.Context {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.notifyCtx != nil {
		return m.notifyCtx
	}
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}
