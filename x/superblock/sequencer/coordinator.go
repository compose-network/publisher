package sequencer

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/compose-network/publisher/x/consensus"
	"github.com/compose-network/publisher/x/transport"
	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/rs/zerolog"
)

// SequencerCoordinator coordinates sequencer SBCP operations
type SequencerCoordinator struct {
	mu      sync.RWMutex
	config  Config
	chainID compose.ChainID
	log     zerolog.Logger

	// Core components
	stateMachine   *StateMachine
	blockBuilder   *BlockBuilder
	messageRouter  *MessageRouter
	scpIntegration *SCPIntegration

	// Dependencies
	consensusCoord consensus.Coordinator
	transport      transport.Client

	// Miner integration (SDK)
	minerNotifier MinerNotifier
	callbacks     CoordinatorCallbacks

	// Current slot context
	periodID uint64

	// Runtime state
	running bool
	stopCh  chan struct{}

	// Queue StartSC messages that arrive while an SCP instance is active
	// TODO: rethink
	pendingStartSCs []struct {
		from  string
		start *pb.StartInstance
	}
}

// NewSequencerCoordinator creates a new sequencer coordinator
func NewSequencerCoordinator(
	baseConsensus consensus.Coordinator,
	config Config,
	transport transport.Client,
	log zerolog.Logger,
) *SequencerCoordinator {
	coordinator := &SequencerCoordinator{
		config:         config,
		chainID:        config.ChainID,
		log:            log.With().Str("component", "sequencer.coordinator").Logger(),
		consensusCoord: baseConsensus,
		transport:      transport,
		stopCh:         make(chan struct{}),
	}

	// Initialize state machine with callback
	coordinator.stateMachine = NewStateMachine(
		config.ChainID,
		log,
		coordinator.onStateChange,
	)

	// Initialize block builder
	coordinator.blockBuilder = NewBlockBuilder(config.ChainID, log)

	// Initialize SCP integration
	coordinator.scpIntegration = NewSCPIntegration(
		config.ChainID,
		baseConsensus,
		coordinator.stateMachine,
		log,
		coordinator.blockBuilder,
	)

	scpHandler := consensus.NewProtocolHandler(baseConsensus, log)

	// Initialize message router with protocol handlers
	coordinator.messageRouter = NewMessageRouter(nil, scpHandler, log)

	// Bind consensus decision callback directly to the coordinator so lifecycle is unified
	// and external callers (e.g., SDK hosts) don't need to forward decisions.
	if baseConsensus != nil {
		baseConsensus.SetDecisionCallback(coordinator.handleConsensusDecision)
	}

	return coordinator
}

func (sc *SequencerCoordinator) SubmitXTRequest(ctx context.Context, from string, request *pb.XTRequest) error {
	panic("not implemented")
}

func (sc *SequencerCoordinator) Transport() transport.Transport {
	panic("not implemented")
}

// Start starts the sequencer coordinator
func (sc *SequencerCoordinator) Start(ctx context.Context) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.running {
		return fmt.Errorf("coordinator already running")
	}

	sc.log.Info().Msg("Starting sequencer coordinator")

	if err := sc.consensusCoord.Start(ctx); err != nil {
		return fmt.Errorf("failed to start consensus coordinator: %w", err)
	}

	sc.running = true

	sc.log.Info().
		Str("chain_id", fmt.Sprintf("%x", sc.chainID)).
		Str("state", sc.stateMachine.GetCurrentState().String()).
		Msg("Sequencer coordinator started")

	return nil
}

// Stop stops the sequencer coordinator
func (sc *SequencerCoordinator) Stop(ctx context.Context) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if !sc.running {
		return nil
	}

	sc.log.Info().Msg("Stopping sequencer coordinator")

	close(sc.stopCh)

	if err := sc.consensusCoord.Stop(ctx); err != nil {
		sc.log.Warn().Err(err).Msg("Failed to stop consensus coordinator gracefully")
	}

	sc.running = false

	sc.log.Info().Msg("Sequencer coordinator stopped")
	return nil
}

// HandleMessage routes messages through the message router
func (sc *SequencerCoordinator) HandleMessage(ctx context.Context, from string, msg *pb.Message) error {
	return sc.messageRouter.Route(ctx, from, msg)
}

// handleStartPeriod processes StartPeriod from SP
func (sc *SequencerCoordinator) handleStartPeriod(
	ctx context.Context,
	from string,
	rb *pb.StartPeriod,
) error {
	panic("not implemented")
}

// handleRollback processes rollback from SP
func (sc *SequencerCoordinator) handleRollback(
	ctx context.Context,
	from string,
	rb *pb.Rollback,
) error {
	panic("not implemented")
}

// handleStartInstance processes StartInstance from SP
func (sc *SequencerCoordinator) handleStartInstance(
	ctx context.Context,
	from string,
	startInstance *pb.StartInstance) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if startInstance.PeriodId != sc.periodID {
		sc.log.Warn().
			Uint64("period_id", startInstance.PeriodId).
			Uint64("current_period_id", sc.periodID).
			Msg("StartSC for wrong slot")
		return nil
	}

	if sc.stateMachine.GetCurrentState() != StateBuildingFree {
		// Queue for later processing once the current SCP instance completes
		sc.pendingStartSCs = append(sc.pendingStartSCs, struct {
			from  string
			start *pb.StartInstance
		}{from: from, start: startInstance})
		sc.log.Warn().
			Str("state", sc.stateMachine.GetCurrentState().String()).
			Int("queued", len(sc.pendingStartSCs)).
			Msg("StartSC received while locked; queued for later")
		return nil
	}

	// Enforce StartSC ordering: previous instance must be decided and sequence must be monotonic
	if sc.scpIntegration.GetActiveCount() > 0 {
		sc.pendingStartSCs = append(sc.pendingStartSCs, struct {
			from  string
			start *pb.StartInstance
		}{from: from, start: startInstance})
		sc.log.Warn().
			Uint64("sequence", startInstance.SequenceNumber).
			Int("queued", len(sc.pendingStartSCs)).
			Msg("StartSC queued: previous instance undecided")
		return nil
	}

	requiredSeq := uint64(0)
	if lastSeq, ok := sc.scpIntegration.GetLastDecidedSequenceNumber(); ok {
		requiredSeq = lastSeq + 1
	}
	if startInstance.SequenceNumber != requiredSeq {
		sc.log.Warn().
			Uint64("got_seq", startInstance.SequenceNumber).
			Uint64("required_seq", requiredSeq).
			Msg("StartSC ignored: non-monotonic sequence")
		return nil
	}

	sc.log.Info().
		Str("instance_ID", string(startInstance.InstanceId)).
		Uint64("sequence", startInstance.SequenceNumber).
		Msg("Starting SCP for cross-chain transaction")

	// Transition to Building-Locked
	if err := sc.stateMachine.TransitionTo(
		StateBuildingLocked,
		startInstance.PeriodId,
		fmt.Sprintf("StartSC seq=%d", startInstance.SequenceNumber),
	); err != nil {
		return err
	}

	// Handle SCP integration
	if err := sc.scpIntegration.HandleStartSC(ctx, startInstance); err != nil {
		return err
	}

	// Extract our transactions
	myTxs := sc.extractMyTransactions(startInstance.XtRequest)

	var instanceID compose.InstanceID
	copy(instanceID[:], startInstance.InstanceId)

	var voteResult = true

	if sc.callbacks.SimulateAndVote != nil && len(myTxs) > 0 {
		success, err := sc.callbacks.SimulateAndVote(ctx, startInstance.XtRequest, instanceID)
		if err != nil {
			sc.log.Error().Err(err).Str("instance_id", instanceID.String()).Msg("Simulation failed")
			voteResult = false
		} else {
			voteResult = success
		}
	} else if len(myTxs) > 0 {
		// TODO: handle this case
		sc.log.Warn().
			Str("instance_id", instanceID.String()).
			Msg("No simulation callback configured, voting true blindly")
	}

	// Send vote based on a simulation result
	vote := &pb.Vote{
		ChainId:    uint64(sc.chainID),
		InstanceId: startInstance.InstanceId,
		Vote:       voteResult,
	}

	msg := &pb.Message{
		SenderId: fmt.Sprintf("seq-%x", sc.chainID),
		Payload:  &pb.Message_Vote{Vote: vote},
	}

	if err := sc.transport.Send(ctx, msg); err != nil {
		sc.log.Error().Err(err).Msg("Failed to send vote to SP")
		return err
	}

	sc.log.Info().
		Str("instance_id", string(startInstance.InstanceId)).
		Bool("vote", voteResult).
		Msg("Sent vote to SP based on simulation")

	return nil
}

// Helper to extract our transactions
func (sc *SequencerCoordinator) extractMyTransactions(xtReq *pb.XTRequest) [][]byte {
	myTxs := make([][]byte, 0)

	for _, txReq := range xtReq.TransactionRequests {
		if compose.ChainID(txReq.ChainId) == sc.chainID {
			myTxs = append(myTxs, txReq.Transaction...)
		}
	}

	return myTxs
}

// onStateChange handles actions on state transitions
func (sc *SequencerCoordinator) onStateChange(from, to State, slot uint64, reason string) {
	// Handle state-specific actions
	switch to {
	case StateBuildingFree:
		// Ready to accept local transactions
		sc.log.Debug().Msg("Ready to accept local transactions")

	case StateBuildingLocked:
		// SCP in progress - no local tx acceptance
		sc.log.Debug().Msg("SCP in progress - blocking local transactions")

	case StateSubmission:
		// Block sealing in progress
		sc.log.Debug().Msg("Block sealing in progress")

	case StateWaiting:
		// Waiting for next slot
		sc.log.Debug().Msg("Waiting for next slot")
	}

	// Notify miner about state changes
	if sc.minerNotifier != nil {
		if err := sc.minerNotifier.NotifyStateChange(from, to, slot); err != nil {
			sc.log.Error().Err(err).Msg("Failed to notify miner of state change")
		}
	}
}

// Interface implementations

// Consensus returns the underlying consensus coordinator
func (sc *SequencerCoordinator) Consensus() consensus.Coordinator {
	return sc.consensusCoord
}

func (sc *SequencerCoordinator) GetCurrentSlot() uint64 {
	return atomic.LoadUint64(&sc.periodID)
}

func (sc *SequencerCoordinator) GetState() State {
	return sc.stateMachine.GetCurrentState()
}

func (sc *SequencerCoordinator) GetStats() map[string]interface{} {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	stats := map[string]interface{}{
		"running":       sc.running,
		"chain_id":      fmt.Sprintf("%x", sc.chainID),
		"current_slot":  atomic.LoadUint64(&sc.periodID),
		"current_state": sc.stateMachine.GetCurrentState().String(),
		"transitions":   len(sc.stateMachine.GetTransitions()),
	}

	// Add block builder stats
	if sc.blockBuilder != nil {
		builderStats := sc.blockBuilder.GetDraftStats()
		stats["block_builder"] = builderStats
	}

	// Add SCP stats
	if sc.scpIntegration != nil {
		activeContexts := sc.scpIntegration.GetActiveContexts()
		stats["active_scp_instances"] = len(activeContexts)
	}

	// Add message router stats
	if sc.messageRouter != nil {
		routerStats := sc.messageRouter.GetStats()
		stats["message_router"] = routerStats
	}

	return stats
}

// GetActiveSCPInstanceCount returns the number of active SCP instances
func (sc *SequencerCoordinator) GetActiveSCPInstanceCount() int {
	if sc.scpIntegration != nil {
		return sc.scpIntegration.GetActiveCount()
	}
	return 0
}

// BlockLifecycleManager implementation

// OnBlockBuildingStart is called when block building starts
// TODO: rethink lock, it blocks ethapi engine
func (sc *SequencerCoordinator) OnBlockBuildingStart(ctx context.Context, slot uint64) error {
	active := 0
	if sc.scpIntegration != nil {
		active = sc.scpIntegration.GetActiveCount()
	}
	state := "unknown"
	if sc.stateMachine != nil {
		state = sc.stateMachine.GetCurrentState().String()
	}
	sc.log.Info().
		Uint64("slot", slot).
		Str("state", state).
		Int("active_scp_instances", active).
		Msg("Block building started")

	if active > 0 {
		sc.log.Info().
			Uint64("slot", slot).
			Msg("Building with in-flight SCP instances")
	}
	return nil
}

// handleConsensusDecision processes the final decision from the consensus layer for a cross-chain
// transaction. It updates the block builder state and manages transaction lifecycle based on whether
// the transaction was committed (decision=true) or aborted (decision=false).
//
// For committed transactions, the block builder includes them in the draft block. For aborted
// transactions, they are immediately removed from both the block builder and the execution layer's
// pending pool to ensure they cannot be included in any block. This guarantees atomic inclusion
// semantics: transactions are either fully included or fully excluded, with no partial states.
//
// After processing the decision, if the coordinator has returned to Building-Free state and there
// are queued cross-chain transactions waiting, the next one is automatically started.
func (sc *SequencerCoordinator) handleConsensusDecision(ctx context.Context, instanceID compose.InstanceID, decision bool) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.log.Info().
		Str("instance_id", instanceID.String()).
		Bool("decision", decision).
		Msg("Processing consensus decision at coordinator")

	// HandleDecision is idempotent - if RequestSeal already processed this, it's a no-op
	if err := sc.scpIntegration.HandleDecision(instanceID, decision); err != nil {
		// If context not found, it means RequestSeal already handled this decision
		sc.log.Debug().
			Err(err).
			Str("instance_id", instanceID.String()).
			Msg("SCP context already processed (likely by RequestSeal)")
		return nil
	}

	// For aborted transactions, immediately invoke cleanup callback to remove from pending pool.
	// This ensures the transaction cannot be committed in blocks built before RequestSeal arrives.
	if !decision && sc.callbacks.CleanupAbortedTransaction != nil {
		if err := sc.callbacks.CleanupAbortedTransaction(ctx, instanceID); err != nil {
			sc.log.Warn().Err(err).Str("instance_id", instanceID.String()).Msg("Cleanup callback failed for aborted transaction")
		}
	}

	// If we returned to Building-Free and have queued StartSCs, process the next one
	if sc.stateMachine.GetCurrentState() == StateBuildingFree && len(sc.pendingStartSCs) > 0 {
		next := sc.pendingStartSCs[0]
		sc.pendingStartSCs = sc.pendingStartSCs[1:]
		sc.log.Info().
			Int("remaining", len(sc.pendingStartSCs)).
			Uint64("slot", sc.periodID).
			Msg("Starting next queued StartSC after decision")
		// Drop lock while invoking handler to avoid deadlocks and allow nested transitions
		sc.mu.Unlock()
		defer sc.mu.Lock()
		return sc.handleStartInstance(ctx, next.from, next.start)
	}

	return nil
}

// PrepareTransactionsForBlock prepares transactions for block inclusion
func (sc *SequencerCoordinator) PrepareTransactionsForBlock(ctx context.Context, slot uint64) error {
	if slot != sc.periodID {
		return fmt.Errorf("preparing for wrong slot: current=%d, requested=%d", sc.periodID, slot)
	}

	sc.log.Debug().
		Uint64("slot", slot).
		Msg("Preparing transactions for block")

	// Prepare any coordination transactions through block builder
	if sc.blockBuilder != nil {
		// BlockBuilder should handle coordination transaction preparation
		sc.log.Debug().Msg("Block builder preparation delegated")
	}

	return nil
}

// GetOrderedTransactionsForBlock returns transactions in correct order for block
func (sc *SequencerCoordinator) GetOrderedTransactionsForBlock(ctx context.Context) ([]*pb.TransactionRequest, error) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	// Delegate to block builder for transaction ordering
	if sc.blockBuilder != nil {
		sc.log.Debug().Msg("Getting ordered transactions from block builder")
		// This would need to be implemented in BlockBuilder
		// For now, return empty slice
		return []*pb.TransactionRequest{}, nil
	}

	return []*pb.TransactionRequest{}, nil
}

// CallbackManager implementation

// SetCallbacks sets the coordinator callbacks
func (sc *SequencerCoordinator) SetCallbacks(callbacks CoordinatorCallbacks) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.callbacks = callbacks
	sc.log.Debug().Msg("Coordinator callbacks set")
}

// SetMinerNotifier sets the miner notifier
func (sc *SequencerCoordinator) SetMinerNotifier(notifier MinerNotifier) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.minerNotifier = notifier
	sc.log.Debug().Msg("Miner notifier set")
}
