package consensus

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/compose-network/specs/compose"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"

	pb "github.com/compose-network/specs/compose/proto"
)

// coordinator implements the Coordinator interface
type coordinator struct {
	config       Config
	stateManager *StateManager
	callbackMgr  *CallbackManager
	metrics      MetricsRecorder
	log          zerolog.Logger

	// Track committed xTs already sent with a block to avoid duplicates
	sentMu  sync.Mutex
	sentMap map[compose.InstanceID]bool

	// Lifecycle management
	started      atomic.Bool
	stopped      atomic.Bool
	stopCh       chan struct{}
	wg           sync.WaitGroup
	shutdownOnce sync.Once
}

// New creates a new coordinator instance
func New(log zerolog.Logger, config Config) Coordinator {
	return NewWithMetrics(log, config, NewMetrics())
}

// NewWithMetrics creates a new coordinator instance with custom metrics recorder
// TODO: check best practices for metrics recorder
func NewWithMetrics(log zerolog.Logger, config Config, metrics MetricsRecorder) Coordinator {
	logger := log.With().
		Str("component", "consensus-coordinator").
		Str("role", config.Role.String()).
		Str("node_id", config.NodeID).
		Logger()

	return &coordinator{
		config:       config,
		stateManager: NewStateManager(),
		callbackMgr:  NewCallbackManager(30*time.Second, logger),
		metrics:      metrics,
		log:          logger,
		sentMap:      make(map[compose.InstanceID]bool),
	}
}

// OnBlockCommitted selects committed xTs not yet sent and invokes block callback.
// Used by execution-integrated path (geth types.Block).
func (c *coordinator) OnBlockCommitted(ctx context.Context, block *types.Block) error {
	active := c.stateManager.GetAllActiveIDs()
	instanceIDs := make([]compose.InstanceID, 0)

	for _, id := range active {
		state, ok := c.stateManager.GetState(id)
		if !ok {
			continue
		}
		if state.GetDecision() != StateCommit {
			continue
		}
		c.sentMu.Lock()
		already := c.sentMap[id]
		c.sentMu.Unlock()
		if already {
			continue
		}
		instanceIDs = append(instanceIDs, id)
	}

	if len(instanceIDs) == 0 {
		return nil
	}

	// Invoke block callback
	c.callbackMgr.InvokeBlock(ctx, block, instanceIDs)

	// Mark as sent
	c.sentMu.Lock()
	for _, id := range instanceIDs {
		c.sentMap[id] = true
	}
	c.sentMu.Unlock()

	c.log.Info().
		Int("xt_count", len(instanceIDs)).
		Str("block_hash", block.Hash().Hex()).
		Msg("OnBlockCommitted sent committed xTs")

	return nil
}

func (c *coordinator) StartTransaction(ctx context.Context, from string, xtReq *pb.XTRequest) error {
	// TODO: fetch instance ID from request
	var instanceID compose.InstanceID

	chainIDs := make(map[compose.ChainID]struct{})
	for _, txRequest := range xtReq.TransactionRequests {
		chainID := txRequest.GetChainId()
		chainIDs[compose.ChainID(chainID)] = struct{}{}
	}

	if len(chainIDs) == 0 {
		return fmt.Errorf("no participating chains found")
	}

	state, err := c.stateManager.AddState(instanceID, xtReq, chainIDs)
	if err != nil {
		return err
	}

	// Timeout only for leader; followers rely on the SP decision
	if c.config.Role == Leader {
		state.Timer = time.AfterFunc(c.config.Timeout, func() {
			c.handleTimeout(instanceID)
		})
	}

	c.metrics.RecordTransactionStarted(len(chainIDs))

	c.log.Info().
		Str("instance_id", instanceID.String()).
		Int("participating_chains", len(chainIDs)).
		Dur("timeout", c.config.Timeout).
		Msg("Started 2PC transaction")

	// Invoke start callback
	c.callbackMgr.InvokeStart(ctx, from, xtReq)

	return nil
}

// handleTimeout handles transaction timeout
func (c *coordinator) handleTimeout(instanceID compose.InstanceID) {
	state, exists := c.stateManager.GetState(instanceID)
	if !exists {
		return
	}

	if state.GetDecision() == StateUndecided {
		c.log.Warn().
			Str("instance_id", instanceID.String()).
			Dur("timeout", c.config.Timeout).
			Msg("Transaction timed out")

		c.metrics.RecordTimeout()
		c.handleAbort(instanceID, state)
	}
}

func (c *coordinator) RecordVote(instanceID compose.InstanceID, chainID compose.ChainID, vote bool) (DecisionState, error) {
	state, exists := c.stateManager.GetState(instanceID)
	if !exists {
		return StateUndecided, fmt.Errorf("transaction %s not found", instanceID)
	}

	if state.GetDecision() != StateUndecided {
		return state.GetDecision(), nil
	}

	state.mu.RLock()
	_, isParticipant := state.ParticipatingChains[chainID]
	state.mu.RUnlock()

	if !isParticipant {
		return StateUndecided, fmt.Errorf("chain %d not participating in transaction %s", chainID, instanceID)
	}

	// Add vote atomically
	if !state.AddVote(chainID, vote) {
		return StateUndecided, fmt.Errorf("chain %d already voted for transaction %s", chainID, instanceID)
	}

	voteLatency := time.Since(state.StartTime)
	c.metrics.RecordVote(chainID, vote, voteLatency)

	c.log.Info().
		Str("instance_id", instanceID.String()).
		Uint64("chain", uint64(chainID)).
		Bool("vote", vote).
		Int("votes_recorded", state.GetVoteCount()).
		Int("votes_required", state.GetParticipantCount()).
		Msg("Recorded vote")

	// Handle abort immediately
	if !vote {
		return c.handleAbort(instanceID, state), nil
	}

	// Check for commit (leader only)
	if c.config.Role == Leader {
		if state.GetVoteCount() == state.GetParticipantCount() {
			return c.handleCommit(instanceID, state), nil
		}
	} else {
		// Follower broadcasts vote
		c.callbackMgr.InvokeVote(instanceID, vote, voteLatency)
	}

	return StateUndecided, nil
}

// handleAbort handles an abort decision
func (c *coordinator) handleAbort(instanceID compose.InstanceID, state *TwoPCState) DecisionState {
	state.SetDecision(StateAbort)

	if state.Timer != nil {
		state.Timer.Stop()
	}

	duration := time.Since(state.StartTime)
	c.metrics.RecordTransactionCompleted(StateAbort.String(), duration)

	if c.config.Role == Leader {
		c.callbackMgr.InvokeDecision(instanceID, false, duration)
	} else {
		c.callbackMgr.InvokeVote(instanceID, false, duration)
	}

	// Schedule cleanup
	time.AfterFunc(5*time.Minute, func() {
		c.stateManager.RemoveState(instanceID)
	})

	return StateAbort
}

// handleCommit handles a commit decision
func (c *coordinator) handleCommit(instanceID compose.InstanceID, state *TwoPCState) DecisionState {
	state.SetDecision(StateCommit)

	if state.Timer != nil {
		state.Timer.Stop()
	}

	duration := time.Since(state.StartTime)
	c.metrics.RecordTransactionCompleted(StateCommit.String(), duration)

	c.callbackMgr.InvokeDecision(instanceID, true, duration)

	// Schedule cleanup
	time.AfterFunc(5*time.Minute, func() {
		c.stateManager.RemoveState(instanceID)
	})

	return StateCommit
}

// RecordDecision processes a decision (for followers)
func (c *coordinator) RecordDecision(instanceID compose.InstanceID, decision bool) error {
	if c.config.Role != Follower {
		return fmt.Errorf("only followers can record decisions, current role: %s", c.config.Role)
	}

	state, exists := c.stateManager.GetState(instanceID)
	if !exists {
		c.log.Debug().
			Str("instance_id", instanceID.String()).
			Bool("decision", decision).
			Msg("Received decision for unknown transaction")
		return nil
	}

	if state.GetDecision() != StateUndecided {
		c.log.Debug().
			Str("instance_id", instanceID.String()).
			Bool("decision", decision).
			Msg("Received decision for already decided transaction")
		return nil
	}

	// Set decision
	if decision {
		state.SetDecision(StateCommit)
	} else {
		state.SetDecision(StateAbort)
	}

	// Stop timer
	if state.Timer != nil {
		state.Timer.Stop()
	}

	duration := time.Since(state.StartTime)
	c.metrics.RecordTransactionCompleted(state.GetDecision().String(), duration)

	c.log.Info().
		Str("instance_id", instanceID.String()).
		Bool("decision", decision).
		Dur("duration", duration).
		Msg("Recorded decision")

	// Notify the sequencer coordinator about the decision to allow state transitions and
	// processing of queued transactions. Transaction cleanup is handled separately by the
	// RequestSeal handler to avoid race conditions.
	c.callbackMgr.InvokeDecision(instanceID, decision, duration)

	// Schedule cleanup
	time.AfterFunc(5*time.Minute, func() {
		c.stateManager.RemoveState(instanceID)
	})

	return nil
}

// GetTransactionState returns the current state of a transaction
func (c *coordinator) GetTransactionState(instanceID compose.InstanceID) (DecisionState, error) {
	state, exists := c.stateManager.GetState(instanceID)
	if !exists {
		return StateUndecided, fmt.Errorf("transaction %s not found", instanceID.String())
	}

	return state.GetDecision(), nil
}

// GetActiveTransactions returns all active transaction IDs
func (c *coordinator) GetActiveTransactions() []compose.InstanceID {
	return c.stateManager.GetAllActiveIDs()
}

// GetState retrieves a transaction state
func (c *coordinator) GetState(instanceID compose.InstanceID) (*TwoPCState, bool) {
	return c.stateManager.GetState(instanceID)
}

// RecordMailboxMessage records a Mailbox message for a transaction
func (c *coordinator) RecordMailboxMessage(mailboxMsg *pb.MailboxMessage) error {
	var instanceID compose.InstanceID
	copy(instanceID[:], mailboxMsg.InstanceId)
	state, exists := c.stateManager.GetState(instanceID)
	if !exists {
		return fmt.Errorf("transaction %s not found", instanceID.String())
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	sourceChainID := compose.ChainID(mailboxMsg.SourceChain)
	if _, isParticipant := state.ParticipatingChains[sourceChainID]; !isParticipant {
		return fmt.Errorf("chain %d not participating in transaction %s", mailboxMsg.SourceChain, instanceID)
	}

	// Add message to queue
	messages, ok := state.MailboxMessages[sourceChainID]
	if !ok {
		messages = make([]*pb.MailboxMessage, 0)
	}
	messages = append(messages, mailboxMsg)
	state.MailboxMessages[sourceChainID] = messages

	c.log.Info().
		Str("instance_id", instanceID.String()).
		Uint64("chain_id", mailboxMsg.SourceChain).
		Msg("Recorded Mailbox message")

	return nil
}

// ConsumeMailboxMessage consumes a Mailbox message from the queue
func (c *coordinator) ConsumeMailboxMessage(
	instanceID compose.InstanceID, sourceChainID compose.ChainID,
) (*pb.MailboxMessage, error) {
	state, exists := c.stateManager.GetState(instanceID)
	if !exists {
		return nil, fmt.Errorf("transaction %s not found", instanceID.String())
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if _, isParticipant := state.ParticipatingChains[sourceChainID]; !isParticipant {
		return nil, fmt.Errorf("chain %d not participating in transaction %s", sourceChainID, instanceID.String())
	}

	messages, ok := state.MailboxMessages[sourceChainID]
	if !ok || len(messages) == 0 {
		return nil, fmt.Errorf("no messages available for chain %d in transaction %s", sourceChainID, instanceID.String())
	}

	// Pop first message
	message := messages[0]
	state.MailboxMessages[sourceChainID] = messages[1:]

	if len(state.MailboxMessages[sourceChainID]) == 0 {
		delete(state.MailboxMessages, sourceChainID)
	}

	return message, nil
}

// SetStartCallback sets the start callback
func (c *coordinator) SetStartCallback(fn StartFn) {
	c.callbackMgr.SetStartCallback(fn)
}

// SetVoteCallback sets the vote callback
func (c *coordinator) SetVoteCallback(fn VoteFn) {
	c.callbackMgr.SetVoteCallback(fn)
}

// SetDecisionCallback sets the decision callback
func (c *coordinator) SetDecisionCallback(fn DecisionFn) {
	c.callbackMgr.SetDecisionCallback(fn)
}

// SetBlockCallback sets the block callback
func (c *coordinator) SetBlockCallback(fn BlockFn) {
	c.callbackMgr.SetBlockCallback(fn)
}

// Start initializes and starts the coordinator
func (c *coordinator) Start(ctx context.Context) error {
	if c.started.Load() {
		return fmt.Errorf("coordinator already started")
	}

	c.started.Store(true)
	c.stopCh = make(chan struct{})

	c.log.Info().
		Str("node_id", c.config.NodeID).
		Str("role", c.config.Role.String()).
		Msg("Consensus coordinator starting")

	c.log.Info().Msg("Consensus coordinator started successfully")
	return nil
}

// Stop gracefully stops the coordinator
func (c *coordinator) Stop(ctx context.Context) error {
	if c.stopped.Load() {
		return nil
	}

	c.log.Info().Msg("Consensus coordinator stopping...")
	c.stopped.Store(true)

	if c.stopCh != nil {
		close(c.stopCh)
	}

	done := make(chan struct{})

	go func() {
		c.wg.Wait()
		c.shutdownOnce.Do(func() {
			c.stateManager.Shutdown()
		})
		close(done)
	}()

	select {
	case <-done:
		c.log.Info().Msg("Consensus coordinator stopped gracefully")
		return nil
	case <-ctx.Done():
		c.log.Warn().Msg("Consensus coordinator stop timed out, forcing shutdown")
		c.shutdownOnce.Do(func() {
			c.stateManager.Shutdown()
		})
		return ctx.Err()
	}
}

// Stopped returns true if the coordinator has been stopped
func (c *coordinator) Stopped() bool {
	return c.stopped.Load()
}
