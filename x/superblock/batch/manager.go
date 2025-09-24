package batch

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/slot"
)

// BatchState represents the current state of a batch
type BatchState string

const (
	StateIdle       BatchState = "idle"
	StateCollecting BatchState = "collecting" // Collecting L2 blocks
	StateProving    BatchState = "proving"    // Generating proofs
	StateCompleted  BatchState = "completed"  // Batch completed and sent to SP
	StateFailed     BatchState = "failed"     // Batch failed
)

// BatchInfo holds information about a batch
type BatchInfo struct {
	ID           uint64           `json:"id"`
	State        BatchState       `json:"state"`
	StartEpoch   uint64           `json:"start_epoch"`
	StartTime    time.Time        `json:"start_time"`
	EndTime      *time.Time       `json:"end_time,omitempty"`
	StartSlot    uint64           `json:"start_slot"`
	EndSlot      *uint64          `json:"end_slot,omitempty"`
	SlotCount    uint64           `json:"slot_count"`
	ChainID      uint32           `json:"chain_id"`
	Blocks       []BatchBlockInfo `json:"blocks"`
	ProofJobID   *string          `json:"proof_job_id,omitempty"`
	ErrorMessage *string          `json:"error_message,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

// BatchBlockInfo represents a block within a batch
type BatchBlockInfo struct {
	SlotNumber   uint64      `json:"slot_number"`
	BlockNumber  uint64      `json:"block_number"`
	BlockHash    common.Hash `json:"block_hash"`
	Timestamp    time.Time   `json:"timestamp"`
	TxCount      int         `json:"tx_count"`
	IncludedXTxs []string    `json:"included_xtxs,omitempty"`
}

// BatchEvent represents events during batch lifecycle
type BatchEvent struct {
	Type      string      `json:"type"`
	BatchID   uint64      `json:"batch_id"`
	Data      interface{} `json:"data,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// Manager coordinates batch lifecycle with L1 synchronization and slot timing
type Manager struct {
	mu          sync.RWMutex
	log         zerolog.Logger
	chainID     uint32
	slotManager *slot.Manager
	l1Listener  *L1Listener

	// Current state
	currentBatch     *BatchInfo
	completedBatches map[uint64]*BatchInfo
	batchCounter     uint64

	// Configuration
	maxBatchSize uint64        // Maximum slots per batch
	batchTimeout time.Duration // Timeout for batch completion

	// Event channels
	eventCh   chan BatchEvent
	triggerCh chan BatchTrigger

	// Control
	ctx    context.Context
	cancel context.CancelFunc
}

// ManagerConfig holds configuration for the batch manager
type ManagerConfig struct {
	ChainID      uint32        `mapstructure:"chain_id"       yaml:"chain_id"`
	MaxBatchSize uint64        `mapstructure:"max_batch_size" yaml:"max_batch_size"` // Max slots per batch
	BatchTimeout time.Duration `mapstructure:"batch_timeout"  yaml:"batch_timeout"`  // Timeout for batch
}

// DefaultManagerConfig returns sensible defaults
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		MaxBatchSize: 320,              // 10 epochs * 32 slots/epoch = ~64 minutes
		BatchTimeout: 90 * time.Minute, // Allow extra time for proof generation
	}
}

// NewManager creates a new batch manager
func NewManager(
	cfg ManagerConfig,
	slotMgr *slot.Manager,
	l1Listener *L1Listener,
	log zerolog.Logger,
) (*Manager, error) {
	if slotMgr == nil {
		return nil, fmt.Errorf("slot manager is required")
	}
	if l1Listener == nil {
		return nil, fmt.Errorf("L1 listener is required")
	}

	logger := log.With().Str("component", "batch-manager").Logger()
	ctx, cancel := context.WithCancel(context.Background())

	mgr := &Manager{
		log:              logger,
		chainID:          cfg.ChainID,
		slotManager:      slotMgr,
		l1Listener:       l1Listener,
		completedBatches: make(map[uint64]*BatchInfo),
		maxBatchSize:     cfg.MaxBatchSize,
		batchTimeout:     cfg.BatchTimeout,
		eventCh:          make(chan BatchEvent, 100),
		triggerCh:        make(chan BatchTrigger, 10),
		ctx:              ctx,
		cancel:           cancel,
	}

	if mgr.maxBatchSize == 0 {
		mgr.maxBatchSize = 320
	}
	if mgr.batchTimeout == 0 {
		mgr.batchTimeout = 90 * time.Minute
	}

	logger.Info().
		Uint32("chain_id", mgr.chainID).
		Uint64("max_batch_size", mgr.maxBatchSize).
		Dur("batch_timeout", mgr.batchTimeout).
		Msg("Batch manager initialized")

	return mgr, nil
}

// Start begins batch management
func (m *Manager) Start(ctx context.Context) error {
	m.log.Info().Msg("Starting batch manager")

	go m.eventLoop(ctx)

	return nil
}

// Stop gracefully stops the batch manager
func (m *Manager) Stop(ctx context.Context) error {
	m.log.Info().Msg("Stopping batch manager")

	m.cancel()

	// Complete current batch if any
	m.mu.Lock()
	if m.currentBatch != nil && m.currentBatch.State == StateCollecting {
		m.currentBatch.State = StateFailed
		m.currentBatch.ErrorMessage = stringPtr("Manager stopped")
		now := time.Now()
		m.currentBatch.EndTime = &now
		m.currentBatch.UpdatedAt = now
	}
	m.mu.Unlock()

	close(m.eventCh)
	close(m.triggerCh)

	return nil
}

// eventLoop processes batch events and L1 triggers
func (m *Manager) eventLoop(ctx context.Context) {
	m.log.Info().Msg("Batch manager event loop started")

	for {
		select {
		case <-ctx.Done():
			m.log.Info().Msg("Batch manager event loop stopping")
			return
		case <-m.ctx.Done():
			m.log.Info().Msg("Batch manager event loop canceled")
			return

		case trigger := <-m.l1Listener.BatchTriggers():
			m.handleBatchTrigger(trigger)

		case err := <-m.l1Listener.Errors():
			m.log.Error().Err(err).Msg("L1 listener error")
		}
	}
}

// handleBatchTrigger processes batch triggers from L1 listener
func (m *Manager) handleBatchTrigger(trigger BatchTrigger) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.log.Info().
		Uint64("epoch", trigger.TriggerEpoch).
		Bool("should_start", trigger.ShouldStart).
		Msg("Handling batch trigger")

	if trigger.ShouldStart {
		m.startNewBatch(trigger)
	}
}

// startNewBatch initiates a new batch
func (m *Manager) startNewBatch(trigger BatchTrigger) {
	// Complete current batch if exists
	if m.currentBatch != nil && m.currentBatch.State == StateCollecting {
		if err := m.finalizeBatch(); err != nil {
			m.log.Error().Err(err).Msg("Failed to finalize previous batch")
			// Continue with new batch anyway
		}
	}

	m.batchCounter++
	currentSlot := m.slotManager.GetCurrentSlot()

	batch := &BatchInfo{
		ID:         m.batchCounter,
		State:      StateCollecting,
		StartEpoch: trigger.TriggerEpoch,
		StartTime:  trigger.TriggerTime,
		StartSlot:  currentSlot,
		SlotCount:  0,
		ChainID:    m.chainID,
		Blocks:     make([]BatchBlockInfo, 0),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	m.currentBatch = batch

	m.log.Info().
		Uint64("batch_id", batch.ID).
		Uint64("start_epoch", batch.StartEpoch).
		Uint64("start_slot", batch.StartSlot).
		Msg("New batch started")

	// Emit event
	event := BatchEvent{
		Type:      "batch_started",
		BatchID:   batch.ID,
		Data:      batch,
		Timestamp: time.Now(),
	}

	select {
	case m.eventCh <- event:
	default:
		m.log.Warn().Msg("Event channel full, dropping batch_started event")
	}
}

// AddBlock adds a block to the current collecting batch
func (m *Manager) AddBlock(slotNum, blockNum uint64, blockHash common.Hash, txCount int, includedXTxs []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.currentBatch == nil || m.currentBatch.State != StateCollecting {
		return fmt.Errorf("no active collecting batch")
	}

	blockInfo := BatchBlockInfo{
		SlotNumber:   slotNum,
		BlockNumber:  blockNum,
		BlockHash:    blockHash,
		Timestamp:    time.Now(),
		TxCount:      txCount,
		IncludedXTxs: includedXTxs,
	}

	m.currentBatch.Blocks = append(m.currentBatch.Blocks, blockInfo)
	m.currentBatch.SlotCount = slotNum - m.currentBatch.StartSlot + 1
	m.currentBatch.UpdatedAt = time.Now()

	m.log.Debug().
		Uint64("batch_id", m.currentBatch.ID).
		Uint64("slot", slotNum).
		Uint64("block", blockNum).
		Str("hash", blockHash.Hex()).
		Int("tx_count", txCount).
		Uint64("total_slots", m.currentBatch.SlotCount).
		Msg("Block added to batch")

	// Check if batch should be finalized due to size limits
	if m.currentBatch.SlotCount >= m.maxBatchSize {
		m.log.Info().
			Uint64("batch_id", m.currentBatch.ID).
			Uint64("slot_count", m.currentBatch.SlotCount).
			Uint64("max_size", m.maxBatchSize).
			Msg("Batch reached maximum size, finalizing")

		return m.finalizeBatch()
	}

	return nil
}

// finalizeBatch moves the current batch to proving state
func (m *Manager) finalizeBatch() error {
	if m.currentBatch == nil {
		return fmt.Errorf("no current batch to finalize")
	}

	currentSlot := m.slotManager.GetCurrentSlot()
	now := time.Now()

	m.currentBatch.State = StateProving
	m.currentBatch.EndSlot = &currentSlot
	m.currentBatch.EndTime = &now
	m.currentBatch.UpdatedAt = now

	m.log.Info().
		Uint64("batch_id", m.currentBatch.ID).
		Uint64("slot_count", m.currentBatch.SlotCount).
		Int("block_count", len(m.currentBatch.Blocks)).
		Msg("Batch finalized and ready for proving")

	// Emit event
	event := BatchEvent{
		Type:      "batch_finalized",
		BatchID:   m.currentBatch.ID,
		Data:      m.currentBatch,
		Timestamp: time.Now(),
	}

	select {
	case m.eventCh <- event:
	default:
		m.log.Warn().Msg("Event channel full, dropping batch_finalized event")
	}

	// Move to completed batches
	m.completedBatches[m.currentBatch.ID] = m.currentBatch
	m.currentBatch = nil

	return nil
}

// GetCurrentBatch returns information about the current batch
func (m *Manager) GetCurrentBatch() *BatchInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.currentBatch == nil {
		return nil
	}

	// Return a copy
	batch := *m.currentBatch
	batch.Blocks = make([]BatchBlockInfo, len(m.currentBatch.Blocks))
	copy(batch.Blocks, m.currentBatch.Blocks)

	return &batch
}

// GetBatch returns information about a specific batch
func (m *Manager) GetBatch(batchID uint64) *BatchInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.currentBatch != nil && m.currentBatch.ID == batchID {
		batch := *m.currentBatch
		batch.Blocks = make([]BatchBlockInfo, len(m.currentBatch.Blocks))
		copy(batch.Blocks, m.currentBatch.Blocks)
		return &batch
	}

	if completed, exists := m.completedBatches[batchID]; exists {
		batch := *completed
		batch.Blocks = make([]BatchBlockInfo, len(completed.Blocks))
		copy(batch.Blocks, completed.Blocks)
		return &batch
	}

	return nil
}

// UpdateBatchProofJob updates the proof job ID for a batch
func (m *Manager) UpdateBatchProofJob(batchID uint64, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var batch *BatchInfo
	if m.currentBatch != nil && m.currentBatch.ID == batchID {
		batch = m.currentBatch
	} else if completed, exists := m.completedBatches[batchID]; exists {
		batch = completed
	}

	if batch == nil {
		return fmt.Errorf("batch %d not found", batchID)
	}

	batch.ProofJobID = &jobID
	batch.UpdatedAt = time.Now()

	m.log.Info().
		Uint64("batch_id", batchID).
		Str("proof_job_id", jobID).
		Msg("Batch proof job updated")

	return nil
}

// MarkBatchCompleted marks a batch as completed
func (m *Manager) MarkBatchCompleted(batchID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	batch, exists := m.completedBatches[batchID]
	if !exists {
		return fmt.Errorf("batch %d not found", batchID)
	}

	batch.State = StateCompleted
	batch.UpdatedAt = time.Now()

	m.log.Info().
		Uint64("batch_id", batchID).
		Msg("Batch marked as completed")

	// Emit event
	event := BatchEvent{
		Type:      "batch_completed",
		BatchID:   batchID,
		Data:      batch,
		Timestamp: time.Now(),
	}

	select {
	case m.eventCh <- event:
	default:
		m.log.Warn().Msg("Event channel full, dropping batch_completed event")
	}

	return nil
}

// MarkBatchFailed marks a batch as failed with an error message
func (m *Manager) MarkBatchFailed(batchID uint64, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	batch, exists := m.completedBatches[batchID]
	if !exists {
		return fmt.Errorf("batch %d not found", batchID)
	}

	batch.State = StateFailed
	batch.ErrorMessage = &errMsg
	batch.UpdatedAt = time.Now()

	m.log.Error().
		Uint64("batch_id", batchID).
		Str("error", errMsg).
		Msg("Batch marked as failed")

	// Emit event
	event := BatchEvent{
		Type:      "batch_failed",
		BatchID:   batchID,
		Data:      batch,
		Timestamp: time.Now(),
	}

	select {
	case m.eventCh <- event:
	default:
		m.log.Warn().Msg("Event channel full, dropping batch_failed event")
	}

	return nil
}

// Events returns the channel for batch events
func (m *Manager) Events() <-chan BatchEvent {
	return m.eventCh
}

// GetStats returns batch manager statistics
func (m *Manager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := map[string]interface{}{
		"total_batches":    len(m.completedBatches),
		"current_batch_id": nil,
		"batches_by_state": make(map[string]int),
		"chain_id":         m.chainID,
		"max_batch_size":   m.maxBatchSize,
		"batch_timeout":    m.batchTimeout.String(),
	}

	if m.currentBatch != nil {
		stats["current_batch_id"] = m.currentBatch.ID
	}

	stateCount := stats["batches_by_state"].(map[string]int)
	if m.currentBatch != nil {
		stateCount[string(m.currentBatch.State)]++
	}

	for _, batch := range m.completedBatches {
		stateCount[string(batch.State)]++
	}

	return stats
}

// IsCollectingBatch returns true if there's an active batch collecting blocks
func (m *Manager) IsCollectingBatch() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.currentBatch != nil && m.currentBatch.State == StateCollecting
}

// GetBatchesReadyForProving returns batches that are ready for proof generation
func (m *Manager) GetBatchesReadyForProving() []*BatchInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var ready []*BatchInfo
	for _, batch := range m.completedBatches {
		if batch.State == StateProving && batch.ProofJobID == nil {
			batchCopy := *batch
			batchCopy.Blocks = make([]BatchBlockInfo, len(batch.Blocks))
			copy(batchCopy.Blocks, batch.Blocks)
			ready = append(ready, &batchCopy)
		}
	}

	return ready
}

// Helper function
func stringPtr(s string) *string {
	return &s
}
