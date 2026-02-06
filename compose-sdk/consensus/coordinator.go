package consensus

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/compose-network/specs/compose"
	"github.com/rs/zerolog"
)

// Coordinator manages 2PC consensus for cross-chain transactions.
type Coordinator interface {
	// StartTransaction initiates a new 2PC transaction.
	StartTransaction(ctx context.Context, xtID string, participantChains []compose.ChainID, data []byte) error

	// RecordVote records a vote from a participant chain.
	// Returns the current decision state and whether a decision was made.
	RecordVote(xtID string, chainID compose.ChainID, vote bool) (compose.DecisionState, bool, error)

	// RecordDecision records a decision received from the leader (for followers).
	RecordDecision(xtID string, decision bool) error

	// GetTransactionState returns the state of a transaction.
	GetTransactionState(xtID string) (*TransactionState, error)

	// GetActiveTransactions returns IDs of all pending transactions.
	GetActiveTransactions() []string

	// SetStartCallback sets the callback for when a transaction should be broadcast.
	SetStartCallback(fn StartFn)

	// SetVoteCallback sets the callback for when this node should send its vote.
	SetVoteCallback(fn VoteFn)

	// SetDecisionCallback sets the callback for when a decision is made.
	SetDecisionCallback(fn DecisionFn)

	// Start starts the coordinator.
	Start(ctx context.Context) error

	// Stop stops the coordinator gracefully.
	Stop(ctx context.Context) error
}

type coordinator struct {
	mu     sync.RWMutex
	log    zerolog.Logger
	cfg    Config
	states *StateManager

	onStart    StartFn
	onVote     VoteFn
	onDecision DecisionFn

	timers  map[string]*time.Timer
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// New creates a new consensus coordinator.
func New(cfg Config, log zerolog.Logger) Coordinator {
	return &coordinator{
		log:    log.With().Str("component", "consensus").Logger(),
		cfg:    cfg,
		states: NewStateManager(cfg.CleanupPeriod, 10*time.Minute),
		timers: make(map[string]*time.Timer),
		stopCh: make(chan struct{}),
	}
}

func (c *coordinator) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("coordinator already running")
	}
	c.running = true
	c.stopCh = make(chan struct{})
	c.mu.Unlock()

	c.states.Start(ctx)
	c.log.Info().Bool("is_leader", c.cfg.IsLeader).Msg("Consensus coordinator started")
	return nil
}

func (c *coordinator) Stop(ctx context.Context) error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = false
	close(c.stopCh)

	// Cancel all timers
	for _, timer := range c.timers {
		timer.Stop()
	}
	c.timers = make(map[string]*time.Timer)
	c.mu.Unlock()

	c.states.Stop()
	c.wg.Wait()
	c.log.Info().Msg("Consensus coordinator stopped")
	return nil
}

func (c *coordinator) StartTransaction(
	ctx context.Context,
	xtID string,
	participantChains []compose.ChainID,
	data []byte,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return fmt.Errorf("coordinator not running")
	}

	// Check if transaction already exists
	if state := c.states.GetState(xtID); state != nil {
		return fmt.Errorf("transaction %s already exists", xtID)
	}

	// Check max pending
	if total, pending, _, _ := c.states.GetStats(); pending >= c.cfg.MaxPending {
		return fmt.Errorf("max pending transactions reached (%d/%d)", pending, total)
	}

	// Create state
	c.states.AddState(xtID, participantChains, data)

	c.log.Info().
		Str("xt_id", xtID).
		Int("participants", len(participantChains)).
		Msg("Transaction started")

	// Set timeout timer (leader only)
	if c.cfg.IsLeader {
		timer := time.AfterFunc(c.cfg.Timeout, func() {
			c.handleTimeout(xtID)
		})
		c.timers[xtID] = timer
	}

	// Invoke start callback
	if c.onStart != nil {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			if err := c.onStart(xtID, participantChains, data); err != nil {
				c.log.Error().Err(err).Str("xt_id", xtID).Msg("Start callback failed")
			}
		}()
	}

	return nil
}

func (c *coordinator) RecordVote(xtID string, chainID compose.ChainID, vote bool) (compose.DecisionState, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	state := c.states.GetState(xtID)
	if state == nil {
		return StatePending, false, fmt.Errorf("unknown transaction: %s", xtID)
	}

	if state.IsDecided() {
		return state.Decision, false, nil
	}

	// Record the vote
	if !c.states.AddVote(xtID, chainID, vote) {
		// Already voted
		return state.Decision, false, nil
	}

	c.log.Debug().
		Str("xt_id", xtID).
		Uint64("chain_id", uint64(chainID)).
		Bool("vote", vote).
		Int("votes", state.VoteCount()).
		Int("required", len(state.ParticipantChains)).
		Msg("Vote recorded")

	// Invoke vote callback (for followers to send vote to leader)
	if !c.cfg.IsLeader && c.onVote != nil {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			if err := c.onVote(xtID, chainID, vote); err != nil {
				c.log.Error().Err(err).Str("xt_id", xtID).Msg("Vote callback failed")
			}
		}()
	}

	// Leader makes decision when all votes are in or any abort vote received
	if c.cfg.IsLeader {
		if !vote {
			// Immediate abort on any NO vote
			return c.makeDecision(xtID, false)
		}

		state = c.states.GetState(xtID)
		if state != nil && state.AllVotesReceived() && state.AllVotesCommit() {
			return c.makeDecision(xtID, true)
		}
	}

	return StatePending, false, nil
}

func (c *coordinator) RecordDecision(xtID string, decision bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	state := c.states.GetState(xtID)
	if state == nil {
		return fmt.Errorf("unknown transaction: %s", xtID)
	}

	if state.IsDecided() {
		return nil
	}

	decisionState := StateCommit
	if !decision {
		decisionState = StateAbort
	}

	c.states.SetDecision(xtID, decisionState)

	c.log.Info().
		Str("xt_id", xtID).
		Bool("decision", decision).
		Msg("Decision recorded from leader")

	// Invoke decision callback
	if c.onDecision != nil {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			if err := c.onDecision(xtID, decision); err != nil {
				c.log.Error().Err(err).Str("xt_id", xtID).Msg("Decision callback failed")
			}
		}()
	}

	return nil
}

func (c *coordinator) GetTransactionState(xtID string) (*TransactionState, error) {
	state := c.states.GetState(xtID)
	if state == nil {
		return nil, fmt.Errorf("unknown transaction: %s", xtID)
	}
	return state, nil
}

func (c *coordinator) GetActiveTransactions() []string {
	return c.states.GetAllPending()
}

func (c *coordinator) SetStartCallback(fn StartFn) {
	c.mu.Lock()
	c.onStart = fn
	c.mu.Unlock()
}

func (c *coordinator) SetVoteCallback(fn VoteFn) {
	c.mu.Lock()
	c.onVote = fn
	c.mu.Unlock()
}

func (c *coordinator) SetDecisionCallback(fn DecisionFn) {
	c.mu.Lock()
	c.onDecision = fn
	c.mu.Unlock()
}

func (c *coordinator) makeDecision(xtID string, decision bool) (compose.DecisionState, bool, error) {
	decisionState := StateCommit
	if !decision {
		decisionState = StateAbort
	}

	if !c.states.SetDecision(xtID, decisionState) {
		return decisionState, false, nil
	}

	// Cancel timeout timer
	if timer, ok := c.timers[xtID]; ok {
		timer.Stop()
		delete(c.timers, xtID)
	}

	c.log.Info().
		Str("xt_id", xtID).
		Bool("decision", decision).
		Str("state", decisionState.String()).
		Msg("Decision made")

	// Invoke decision callback
	if c.onDecision != nil {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			if err := c.onDecision(xtID, decision); err != nil {
				c.log.Error().Err(err).Str("xt_id", xtID).Msg("Decision callback failed")
			}
		}()
	}

	return decisionState, true, nil
}

func (c *coordinator) handleTimeout(xtID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	state := c.states.GetState(xtID)
	if state == nil || state.IsDecided() {
		return
	}

	c.log.Warn().
		Str("xt_id", xtID).
		Dur("timeout", c.cfg.Timeout).
		Int("votes", state.VoteCount()).
		Int("required", len(state.ParticipantChains)).
		Msg("Transaction timeout, aborting")

	c.states.SetDecision(xtID, StateAbort)

	// Invoke decision callback
	if c.onDecision != nil {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			if err := c.onDecision(xtID, false); err != nil {
				c.log.Error().Err(err).Str("xt_id", xtID).Msg("Decision callback failed")
			}
		}()
	}
}
