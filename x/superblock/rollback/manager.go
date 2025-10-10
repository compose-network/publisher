package rollback

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	pb "github.com/ssvlabs/rollup-shared-publisher/proto/rollup/v1"
	l1events "github.com/ssvlabs/rollup-shared-publisher/x/superblock/l1/events"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/queue"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/registry"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/slot"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/store"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/wal"
	"github.com/ssvlabs/rollup-shared-publisher/x/transport"
	"google.golang.org/protobuf/proto"
)

// Dependencies defines the dependencies required by the rollback manager
type Dependencies struct {
	SuperblockStore store.SuperblockStore
	L2BlockStore    store.L2BlockStore
	RegistryService registry.Service
	XTQueue         queue.XTRequestQueue
	Transport       transport.Transport
	WALManager      wal.Manager
	StateMachine    *slot.StateMachine
	SlotManager     SlotManager
	Logger          zerolog.Logger
}

// SlotManager defines the interface for slot management operations
type SlotManager interface {
	GetCurrentSlot() uint64
}

// ExecutionManager defines the interface for managing execution state
type ExecutionManager interface {
	GetExecutionHistory(slot uint64) (*SlotExecution, bool)
	GetCurrentExecution() *SlotExecution
	SetCurrentExecution(exec *SlotExecution)
	ClearCurrentExecution()
	SyncExecutionFromStateMachine()
	RecordExecutionSnapshot(exec *SlotExecution)
	CleanupExecutionHistory(beforeSlot uint64)
}

// SlotExecution represents a slot execution snapshot
type SlotExecution struct {
	Slot                 uint64
	State                slot.State
	StartTime            time.Time
	NextSuperblockNumber uint64
	LastSuperblockHash   []byte
	ActiveRollups        [][]byte
	ReceivedL2Blocks     map[string]*pb.L2Block
	SCPInstances         map[string]*SCPInstance
	L2BlockRequests      map[string]*pb.L2BlockRequest
	AttemptedRequests    map[string]*queue.QueuedXTRequest
}

// SCPInstance represents a cross-chain transaction SCP instance
type SCPInstance struct {
	XtID                []byte
	Slot                uint64
	SequenceNumber      uint64
	Request             *pb.XTRequest
	ParticipatingChains [][]byte
	Votes               map[string]bool
	Decision            *bool
	StartTime           time.Time
	DecisionTime        *time.Time
}

// Manager orchestrates rollback operations and implements the Handler interface
type Manager struct {
	deps        Dependencies
	recovery    *Recovery
	txHandler   *TransactionHandler
	log         zerolog.Logger
	execManager ExecutionManager
}

// NewManager creates a new rollback manager with the specified dependencies
func NewManager(deps Dependencies, execManager ExecutionManager) *Manager {
	recovery := NewRecovery(deps.SuperblockStore, deps.L2BlockStore, deps.RegistryService, deps.Logger)
	txHandler := NewTransactionHandler(deps.XTQueue, deps.Logger)

	return &Manager{
		deps:        deps,
		recovery:    recovery,
		txHandler:   txHandler,
		log:         deps.Logger.With().Str("component", "rollback.manager").Logger(),
		execManager: execManager,
	}
}

// HandleSuperblockRollback orchestrates the complete rollback process
func (m *Manager) HandleSuperblockRollback(
	ctx context.Context,
	event *l1events.SuperblockEvent,
	rolledBack *store.Superblock,
) error {
	// Validate the rollback event
	if err := m.validateRollbackEvent(event, rolledBack); err != nil {
		return NewValidationError("rollback event validation failed").
			WithCause(err).
			WithSuperblock(event.SuperblockNumber)
	}

	m.log.Info().
		Uint64("superblock_number", event.SuperblockNumber).
		Uint64("slot", rolledBack.Slot).
		Msg("Starting superblock rollback process")

	// Find the last valid superblock to restore from
	lastValid, err := m.recovery.FindLastValidSuperblock(ctx, rolledBack.Number)
	if err != nil {
		return NewRecoveryError("failed to locate last valid superblock").
			WithCause(err).
			WithSuperblock(rolledBack.Number)
	}

	// Compute L2 block requests for restart
	l2Requests, err := m.recovery.ComputeL2BlockRequests(ctx, lastValid)
	if err != nil {
		return NewRecoveryError("failed to compute L2 block requests").
			WithCause(err).
			WithSuperblock(rolledBack.Number)
	}

	// Requeue rolled back transactions
	if err := m.txHandler.RequeueTransactions(ctx, rolledBack.Slot, m.execManager); err != nil {
		m.log.Warn().Err(err).Uint64("slot", rolledBack.Slot).
			Msg("Failed to requeue transactions from rolled-back slot")
	}

	// Calculate restart parameters
	nextSuperblockNumber := uint64(1)
	lastHash := make([]byte, 32)
	if lastValid != nil {
		nextSuperblockNumber = lastValid.Number + 1
		lastHash = lastValid.Hash.Bytes()
	}

	// Determine current slot
	currentSlot := m.determineCurrentSlot(rolledBack.Slot)

	// Write rollback event to WAL if WAL manager is available
	if m.deps.WALManager != nil {
		if err := m.writeRollbackWALEntry(ctx, currentSlot); err != nil {
			m.log.Warn().Err(err).Msg("Failed to write rollback entry to WAL")
		}
	}

	// Broadcast rollback and restart message to sequencers
	if err := m.broadcastRollbackMessage(ctx, currentSlot, nextSuperblockNumber, lastHash, l2Requests); err != nil {
		return NewBroadcastError("failed to broadcast rollback message").
			WithCause(err).
			WithSlot(currentSlot)
	}

	// Reset and seed state machine
	m.resetStateMachine(lastValid, currentSlot, nextSuperblockNumber, lastHash, l2Requests)

	// Update execution context
	m.updateExecutionContext(currentSlot, nextSuperblockNumber, lastHash, l2Requests, rolledBack.Slot)

	m.log.Info().
		Uint64("rolled_back_number", rolledBack.Number).
		Uint64("last_valid_number", func() uint64 {
			if lastValid == nil {
				return 0
			}
			return lastValid.Number
		}()).
		Uint64("next_superblock_number", nextSuperblockNumber).
		Uint64("restart_slot", currentSlot).
		Int("l2_requests", len(l2Requests)).
		Msg("Superblock rollback completed successfully")

	return nil
}

// validateRollbackEvent validates the rollback event for consistency
func (m *Manager) validateRollbackEvent(event *l1events.SuperblockEvent, rolledBack *store.Superblock) error {
	if rolledBack == nil {
		return fmt.Errorf("rolled-back superblock data missing for event %d", event.SuperblockNumber)
	}
	if event.SuperblockNumber == 0 {
		return fmt.Errorf("rollback event for genesis superblock is invalid")
	}
	if rolledBack.Number != event.SuperblockNumber {
		return fmt.Errorf("rolled-back superblock mismatch: event=%d stored=%d",
			event.SuperblockNumber, rolledBack.Number)
	}
	return nil
}

// determineCurrentSlot determines the appropriate current slot for restart
func (m *Manager) determineCurrentSlot(rolledBackSlot uint64) uint64 {
	currentSlot := m.deps.SlotManager.GetCurrentSlot()
	if currentSlot == 0 {
		currentSlot = m.deps.StateMachine.GetCurrentSlot()
	}
	if currentSlot == 0 {
		currentSlot = rolledBackSlot + 1
	}
	return currentSlot
}

// writeRollbackWALEntry writes a rollback event to the WAL
func (m *Manager) writeRollbackWALEntry(ctx context.Context, currentSlot uint64) error {
	if m.deps.WALManager == nil {
		return nil
	}

	data, err := proto.Marshal(&pb.Message{}) // TODO: placeholder
	if err != nil {
		return fmt.Errorf("failed to marshal rollback data: %w", err)
	}

	return m.deps.WALManager.WriteEntry(ctx, &wal.Entry{
		Slot:      currentSlot,
		Type:      wal.EntryRollback,
		Data:      data,
		Timestamp: time.Now(),
	})
}

// broadcastRollbackMessage sends the RollBackAndStartSlot message to all sequencers
func (m *Manager) broadcastRollbackMessage(
	ctx context.Context,
	currentSlot, nextSuperblockNumber uint64,
	lastSuperblockHash []byte,
	requests []*pb.L2BlockRequest,
) error {
	msg := &pb.Message{
		SenderId: "shared-publisher",
		Payload: &pb.Message_RollBackAndStartSlot{
			RollBackAndStartSlot: &pb.RollBackAndStartSlot{
				L2BlocksRequest:      requests,
				CurrentSlot:          currentSlot,
				NextSuperblockNumber: nextSuperblockNumber,
				LastSuperblockHash:   append([]byte(nil), lastSuperblockHash...),
			},
		},
	}

	m.log.Info().
		Uint64("slot", currentSlot).
		Uint64("next_superblock_number", nextSuperblockNumber).
		Int("l2_requests", len(requests)).
		Msg("Broadcasting RollBackAndStartSlot message to sequencers")

	return m.deps.Transport.Broadcast(ctx, msg, "")
}

// resetStateMachine resets and seeds the state machine with rollback parameters
func (m *Manager) resetStateMachine(
	lastValid *store.Superblock,
	currentSlot, nextSuperblockNumber uint64,
	lastHash []byte,
	l2Requests []*pb.L2BlockRequest,
) {
	// Seed last heads if we have a valid superblock
	if lastValid != nil {
		for _, block := range lastValid.L2Blocks {
			m.deps.StateMachine.SeedLastHead(block)
		}
	}

	// Reset and seed the state machine
	m.deps.StateMachine.Reset()
	m.deps.StateMachine.SeedL2BlockRequests(currentSlot, nextSuperblockNumber, lastHash, l2Requests)
}

// updateExecutionContext creates a fresh execution context for the rollback restart
func (m *Manager) updateExecutionContext(
	currentSlot, nextSuperblockNumber uint64,
	lastHash []byte,
	l2Requests []*pb.L2BlockRequest,
	rolledBackSlot uint64,
) {
	// Create active rollups list
	activeRollups := make([][]byte, 0, len(l2Requests))
	for _, req := range l2Requests {
		activeRollups = append(activeRollups, append([]byte(nil), req.ChainId...))
	}

	// Create L2 block request map
	l2BlockRequestMap := make(map[string]*pb.L2BlockRequest, len(l2Requests))
	for _, req := range l2Requests {
		l2BlockRequestMap[string(req.ChainId)] = proto.Clone(req).(*pb.L2BlockRequest)
	}

	// Create fresh execution context
	execution := &SlotExecution{
		Slot:                 currentSlot,
		State:                slot.StateStarting,
		StartTime:            time.Now(),
		NextSuperblockNumber: nextSuperblockNumber,
		LastSuperblockHash:   append([]byte(nil), lastHash...),
		ActiveRollups:        activeRollups,
		ReceivedL2Blocks:     make(map[string]*pb.L2Block),
		SCPInstances:         make(map[string]*SCPInstance),
		L2BlockRequests:      l2BlockRequestMap,
		AttemptedRequests:    make(map[string]*queue.QueuedXTRequest),
	}

	// Set the execution context
	m.execManager.SetCurrentExecution(execution)
	m.execManager.SyncExecutionFromStateMachine()
	m.execManager.RecordExecutionSnapshot(execution)

	// Clean up execution history
	m.execManager.CleanupExecutionHistory(rolledBackSlot)

	// Clear current execution to complete the rollback
	m.execManager.ClearCurrentExecution()
}
