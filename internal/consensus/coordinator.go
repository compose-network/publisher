package consensus

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	pb "github.com/ssvlabs/rollup-shared-publisher/internal/proto"
)

// BroadcastFn is the function type for broadcasting decisions.
type BroadcastFn func(ctx context.Context, xtID uint32, decision bool) error

// Coordinator manages 2PC transactions and their lifecycle.
type Coordinator struct {
	mu      sync.RWMutex
	states  map[uint32]*TwoPCState
	log     zerolog.Logger
	timeout time.Duration
	metrics *Metrics

	broadcastFn BroadcastFn
}

// NewCoordinator creates a new 2PC coordinator.
func NewCoordinator(logger zerolog.Logger) *Coordinator {
	return &Coordinator{
		states:  make(map[uint32]*TwoPCState),
		log:     logger.With().Str("component", "2pc_coordinator").Logger(),
		timeout: 3 * time.Minute,
		metrics: NewMetrics(),
	}
}

// SetBroadcastCallback sets the function to broadcast decisions.
func (c *Coordinator) SetBroadcastCallback(fn BroadcastFn) {
	c.broadcastFn = fn
}

// SetTimeout sets the transaction timeout duration.
func (c *Coordinator) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
}

// StartTransaction initiates a new 2PC transaction.
func (c *Coordinator) StartTransaction(xtID uint32, req *pb.XTRequest, chains map[string]struct{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.states[xtID]; exists {
		return fmt.Errorf("transaction %d already exists", xtID)
	}

	state := NewTwoPCState(xtID, req, chains)
	c.states[xtID] = state

	state.Timer = time.AfterFunc(c.timeout, func() {
		c.handleTimeout(xtID)
	})

	c.metrics.RecordTransactionStarted(len(chains))

	c.log.Info().
		Uint32("xt_id", xtID).
		Int("participating_chains", len(chains)).
		Dur("timeout", c.timeout).
		Msg("Started 2PC transaction")

	return nil
}

// RecordVote processes a vote from a sequencer.
func (c *Coordinator) RecordVote(xtID uint32, chainID string, vote bool) (DecisionState, error) {
	c.mu.Lock()
	state, exists := c.states[xtID]
	if !exists {
		c.mu.Unlock()
		return StateUndecided, fmt.Errorf("transaction %d not found", xtID)
	}
	c.mu.Unlock()

	state.mu.Lock()
	defer state.mu.Unlock()

	if state.Decision != StateUndecided {
		return state.Decision, nil
	}

	if _, isParticipant := state.ParticipatingChains[chainID]; !isParticipant {
		return StateUndecided, fmt.Errorf("chain %s not participating in transaction %d", chainID, xtID)
	}

	if _, hasVoted := state.Votes[chainID]; hasVoted {
		return StateUndecided, fmt.Errorf("chain %s already voted for transaction %d", chainID, xtID)
	}

	state.Votes[chainID] = vote
	voteLatency := time.Since(state.StartTime)
	c.metrics.RecordVote(chainID, vote, voteLatency)

	c.log.Debug().
		Uint32("xt_id", xtID).
		Str("chain", chainID).
		Bool("vote", vote).
		Int("votes_received", len(state.Votes)).
		Int("votes_required", len(state.ParticipatingChains)).
		Msg("Recorded vote")

	if !vote {
		state.Decision = StateAbort
		state.Timer.Stop()
		go c.broadcastDecision(xtID, false, time.Since(state.StartTime))
		return StateAbort, nil
	}

	if len(state.Votes) == len(state.ParticipatingChains) {
		state.Decision = StateCommit
		state.Timer.Stop()
		go c.broadcastDecision(xtID, true, time.Since(state.StartTime))
		return StateCommit, nil
	}

	return StateUndecided, nil
}

// GetTransactionState returns the current state of a transaction.
func (c *Coordinator) GetTransactionState(xtID uint32) (DecisionState, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	state, exists := c.states[xtID]
	if !exists {
		return StateUndecided, fmt.Errorf("transaction %d not found", xtID)
	}

	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.Decision, nil
}

// GetActiveTransactions returns all active transaction IDs.
func (c *Coordinator) GetActiveTransactions() []uint32 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ids := make([]uint32, 0, len(c.states))
	for id := range c.states {
		ids = append(ids, id)
	}
	return ids
}

// handleTimeout handles transaction timeout.
func (c *Coordinator) handleTimeout(xtID uint32) {
	c.mu.Lock()
	state, exists := c.states[xtID]
	if !exists {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	state.mu.Lock()
	if state.Decision == StateUndecided {
		state.Decision = StateAbort
		state.mu.Unlock()

		c.log.Warn().
			Uint32("xt_id", xtID).
			Dur("timeout", c.timeout).
			Msg("Transaction timed out")

		c.metrics.RecordTimeout()
		go c.broadcastDecision(xtID, false, c.timeout)
	} else {
		state.mu.Unlock()
	}
}

// broadcastDecision broadcasts the decision to all sequencers.
func (c *Coordinator) broadcastDecision(xtID uint32, decision bool, duration time.Duration) {
	state := StateCommit
	if !decision {
		state = StateAbort
	}

	c.metrics.RecordTransactionCompleted(state.String(), duration)

	c.log.Info().
		Uint32("xt_id", xtID).
		Bool("decision", decision).
		Dur("duration", duration).
		Msg("Broadcasting 2PC decision")

	if c.broadcastFn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := c.broadcastFn(ctx, xtID, decision); err != nil {
			c.log.Error().
				Err(err).
				Uint32("xt_id", xtID).
				Bool("decision", decision).
				Msg("Failed to broadcast decision")
		} else {
			c.metrics.RecordDecisionBroadcast(decision)
		}
	}

	time.AfterFunc(5*time.Minute, func() {
		c.removeTransaction(xtID)
	})
}

// removeTransaction removes a completed transaction from memory.
func (c *Coordinator) removeTransaction(xtID uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if state, exists := c.states[xtID]; exists {
		if state.Timer != nil {
			state.Timer.Stop()
		}
		delete(c.states, xtID)

		c.log.Debug().
			Uint32("xt_id", xtID).
			Msg("Removed transaction state")
	}
}

// Shutdown gracefully shuts down the coordinator.
func (c *Coordinator) Shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for xtID, state := range c.states {
		if state.Timer != nil {
			state.Timer.Stop()
		}
		c.log.Debug().
			Uint32("xt_id", xtID).
			Msg("Stopping transaction timer during shutdown")
	}

	c.log.Info().
		Int("active_transactions", len(c.states)).
		Msg("Coordinator shutdown complete")
}
