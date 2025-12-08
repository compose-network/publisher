package sequencer

import (
	"context"
	"fmt"
	"sync"

	"github.com/compose-network/publisher/x/consensus"
	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/rs/zerolog"
)

type SCPContext struct {
	InstanceID     compose.InstanceID
	PeriodID       compose.PeriodID
	Request        *pb.XTRequest
	SequenceNumber uint64
	MyTransactions [][]byte
	Decision       *bool
}

type SCPIntegration struct {
	mu           sync.RWMutex
	chainID      compose.ChainID
	consensus    consensus.Coordinator
	stateMachine *StateMachine
	log          zerolog.Logger

	activeContexts map[compose.InstanceID]*SCPContext // instanceID -> context

	// per-slot tracked state
	includedInstanceIDs map[compose.InstanceID]struct{} // instanceIDs for this slot (decided=true)
	// last decided sequence number for monotonic StartSC enforcement
	lastDecidedSeq    uint64
	hasLastDecidedSeq bool
	currentSlot       uint64
	blockBuilder      *BlockBuilder
}

func NewSCPIntegration(
	chainID compose.ChainID,
	consensus consensus.Coordinator,
	stateMachine *StateMachine,
	log zerolog.Logger,
	builder *BlockBuilder,
) *SCPIntegration {
	return &SCPIntegration{
		chainID:             chainID,
		consensus:           consensus,
		stateMachine:        stateMachine,
		log:                 log.With().Str("component", "scp_integration").Logger(),
		activeContexts:      make(map[compose.InstanceID]*SCPContext),
		includedInstanceIDs: make(map[compose.InstanceID]struct{}),
		blockBuilder:        builder,
	}
}

func (si *SCPIntegration) HandleStartInstance(ctx context.Context, startInstance *pb.StartInstance) error {
	var instanceID compose.InstanceID
	copy(instanceID[:], startInstance.InstanceId)

	si.mu.Lock()
	defer si.mu.Unlock()

	// Ensure local consensus state exists for this xT so CIRC
	// messages can be recorded/consumed by the sequencer's coordinator
	if err := si.consensus.StartTransaction(ctx, "sequencer", startInstance.XtRequest); err != nil {
		// Do not fail the flow – log and continue to avoid blocking SBCP.
		// CIRC Record/Consume will clearly error if state is missing.
		si.log.Error().
			Err(err).
			Str("instance_id", instanceID.String()).
			Msg("Failed to start local 2PC state for StartSC")
	} else {
		si.log.Debug().
			Str("instance_id", instanceID.String()).
			Msg("Initialized local 2PC state for StartSC")
	}

	// Create SCP context
	scpCtx := &SCPContext{
		InstanceID:     instanceID,
		PeriodID:       compose.PeriodID(startInstance.PeriodId),
		Request:        startInstance.XtRequest,
		SequenceNumber: startInstance.SequenceNumber,
		MyTransactions: si.extractMyTransactions(startInstance.XtRequest),
	}

	si.activeContexts[instanceID] = scpCtx

	si.log.Info().
		Str("instance_id", instanceID.String()).
		Uint64("sequence", startInstance.SequenceNumber).
		Int("my_txs", len(scpCtx.MyTransactions)).
		Msg("Started SCP context")

	return nil
}

func (si *SCPIntegration) HandleDecision(instanceID compose.InstanceID, decision bool) error {
	si.mu.Lock()
	defer si.mu.Unlock()

	scpCtx, exists := si.activeContexts[instanceID]
	if !exists {
		return fmt.Errorf("no SCP context found for xt_id %s", instanceID.String())
	}

	scpCtx.Decision = &decision

	si.log.Info().
		Str("instance_id", instanceID.String()).
		Bool("decision", decision).
		Msg("SCP decision received")

	// Update block builder with decision for our chain's txs
	if si.blockBuilder != nil {
		if decision {
			_ = si.blockBuilder.AddSCPTransactions(instanceID, scpCtx.MyTransactions, true)
		} else {
			_ = si.blockBuilder.AddSCPTransactions(instanceID, nil, false)
		}
	}

	// Track included InstanceIDs for superset check
	if decision {
		si.includedInstanceIDs[instanceID] = struct{}{}
	} else {
		delete(si.includedInstanceIDs, instanceID)
	}

	// Clean up context after decision
	delete(si.activeContexts, instanceID)

	// If we were the last SCP instance, transition back to Free
	if len(si.activeContexts) == 0 && si.stateMachine.GetCurrentState() == StateBuildingLocked {
		// update last decided sequence for ordering enforcement
		si.lastDecidedSeq = scpCtx.SequenceNumber
		si.hasLastDecidedSeq = true
		return si.stateMachine.TransitionTo(StateBuildingFree, si.stateMachine.GetCurrentSlot(), "SCP completed")
	}

	return nil
}

func (si *SCPIntegration) extractMyTransactions(xtReq *pb.XTRequest) [][]byte {
	myTxs := make([][]byte, 0)

	for _, txReq := range xtReq.TransactionRequests {
		if compose.ChainID(txReq.ChainId) == si.chainID {
			myTxs = append(myTxs, txReq.Transaction...)
		}
	}

	return myTxs
}

func (si *SCPIntegration) GetActiveContexts() map[string]*SCPContext {
	si.mu.RLock()
	defer si.mu.RUnlock()

	result := make(map[string]*SCPContext)
	for k, v := range si.activeContexts {
		result[k.String()] = v
	}

	return result
}

// ResetForSlot clears per-slot SCP tracking
func (si *SCPIntegration) ResetForSlot(slot uint64) {
	si.mu.Lock()
	defer si.mu.Unlock()
	si.currentSlot = slot
	si.activeContexts = make(map[compose.InstanceID]*SCPContext)
	si.includedInstanceIDs = make(map[compose.InstanceID]struct{})
	si.hasLastDecidedSeq = false
}

// GetIncludedInstanceIDs returns hex-encoded xtIDs decided to include in current slot
func (si *SCPIntegration) GetIncludedInstanceIDs() []compose.InstanceID {
	si.mu.RLock()
	defer si.mu.RUnlock()
	out := make([]compose.InstanceID, 0, len(si.includedInstanceIDs))
	for instanceID := range si.includedInstanceIDs {
		out = append(out, instanceID)
	}
	return out
}

// GetLastDecidedSequenceNumber returns the last decided sequence and whether it exists
func (si *SCPIntegration) GetLastDecidedSequenceNumber() (uint64, bool) {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return si.lastDecidedSeq, si.hasLastDecidedSeq
}

// GetActiveCount returns the number of in-flight SCP instances
func (si *SCPIntegration) GetActiveCount() int {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return len(si.activeContexts)
}
