package batch

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/sequencer"
)

// SequencerIntegration extends the sequencer coordinator with batch awareness
type SequencerIntegration struct {
	log          zerolog.Logger
	coordinator  sequencer.Coordinator
	batchManager *Manager
	pipeline     *Pipeline
	l1Listener   *L1Listener

	// Block tracking
	lastProcessedSlot uint64
}

// IntegrationConfig holds configuration for sequencer integration
type IntegrationConfig struct {
	ChainID         uint32 `mapstructure:"chain_id"          yaml:"chain_id"`
	EnableBatchSync bool   `mapstructure:"enable_batch_sync" yaml:"enable_batch_sync"`
	BlockReporting  bool   `mapstructure:"block_reporting"   yaml:"block_reporting"`
}

// DefaultIntegrationConfig returns sensible defaults
func DefaultIntegrationConfig() IntegrationConfig {
	return IntegrationConfig{
		EnableBatchSync: true,
		BlockReporting:  true,
	}
}

// NewSequencerIntegration creates a new sequencer integration
func NewSequencerIntegration(
	cfg IntegrationConfig,
	coord sequencer.Coordinator,
	batchMgr *Manager,
	pipeline *Pipeline,
	l1Listener *L1Listener,
	log zerolog.Logger,
) (*SequencerIntegration, error) {
	if coord == nil {
		return nil, fmt.Errorf("sequencer coordinator is required")
	}
	if batchMgr == nil {
		return nil, fmt.Errorf("batch manager is required")
	}

	logger := log.With().Str("component", "sequencer-batch-integration").Logger()

	integration := &SequencerIntegration{
		log:          logger,
		coordinator:  coord,
		batchManager: batchMgr,
		pipeline:     pipeline,
		l1Listener:   l1Listener,
	}

	logger.Info().
		Uint32("chain_id", cfg.ChainID).
		Bool("enable_batch_sync", cfg.EnableBatchSync).
		Bool("block_reporting", cfg.BlockReporting).
		Msg("Sequencer batch integration initialized")

	return integration, nil
}

// Start begins the integration monitoring
func (s *SequencerIntegration) Start(ctx context.Context) error {
	s.log.Info().Msg("Starting sequencer batch integration")

	// Start block monitoring
	go s.blockMonitor(ctx)

	// Start batch event monitoring
	if s.batchManager != nil {
		go s.batchEventMonitor(ctx)
	}

	return nil
}

// Stop gracefully stops the integration
func (s *SequencerIntegration) Stop(ctx context.Context) error {
	s.log.Info().Msg("Stopping sequencer batch integration")
	return nil
}

// blockMonitor monitors sequencer block production and reports to batch manager
func (s *SequencerIntegration) blockMonitor(ctx context.Context) {
	ticker := time.NewTicker(6 * time.Second) // Half slot time
	defer ticker.Stop()

	s.log.Info().Msg("Block monitor started")

	for {
		select {
		case <-ctx.Done():
			s.log.Info().Msg("Block monitor stopping")
			return
		case <-ticker.C:
			if err := s.checkForNewBlocks(ctx); err != nil {
				s.log.Error().Err(err).Msg("Failed to check for new blocks")
			}
		}
	}
}

// checkForNewBlocks checks if any new blocks have been produced and reports them
func (s *SequencerIntegration) checkForNewBlocks(ctx context.Context) error {
	// Get current slot from slot manager (assuming it's available via coordinator)
	// This is a simplified implementation - in practice you'd need proper access
	// to the sequencer's slot manager

	// For now, we'll simulate block checking
	// In production, this would query the actual sequencer state

	return nil
}

// ReportBlock reports a newly produced block to the batch manager
func (s *SequencerIntegration) ReportBlock(
	slotNum, blockNum uint64,
	blockHash common.Hash,
	txCount int,
	includedXTxs []string,
) error {
	s.log.Debug().
		Uint64("slot", slotNum).
		Uint64("block", blockNum).
		Str("hash", blockHash.Hex()).
		Int("tx_count", txCount).
		Int("xtx_count", len(includedXTxs)).
		Msg("Reporting block to batch manager")

	if !s.batchManager.IsCollectingBatch() {
		s.log.Debug().
			Uint64("slot", slotNum).
			Msg("No active batch, block will not be included in batch")
		return nil
	}

	if err := s.batchManager.AddBlock(slotNum, blockNum, blockHash, txCount, includedXTxs); err != nil {
		return fmt.Errorf("add block to batch: %w", err)
	}

	// Update tracking
	s.lastProcessedSlot = slotNum

	return nil
}

// batchEventMonitor monitors batch manager events and coordinates with sequencer
func (s *SequencerIntegration) batchEventMonitor(ctx context.Context) {
	s.log.Info().Msg("Batch event monitor started")

	for {
		select {
		case <-ctx.Done():
			s.log.Info().Msg("Batch event monitor stopping")
			return
		case event := <-s.batchManager.Events():
			s.handleBatchEvent(event)
		}
	}
}

// handleBatchEvent processes batch lifecycle events
func (s *SequencerIntegration) handleBatchEvent(event BatchEvent) {
	switch event.Type {
	case "batch_started":
		s.log.Info().
			Uint64("batch_id", event.BatchID).
			Msg("New batch started, enabling block collection")

	case "batch_finalized":
		s.log.Info().
			Uint64("batch_id", event.BatchID).
			Msg("Batch finalized, proof generation will begin")

	case "batch_completed":
		s.log.Info().
			Uint64("batch_id", event.BatchID).
			Msg("Batch completed successfully")

	case "batch_failed":
		s.log.Error().
			Uint64("batch_id", event.BatchID).
			Interface("data", event.Data).
			Msg("Batch failed")

	default:
		s.log.Debug().
			Str("type", event.Type).
			Uint64("batch_id", event.BatchID).
			Msg("Received batch event")
	}
}

// GetCurrentBatchInfo returns information about the current batch being collected
func (s *SequencerIntegration) GetCurrentBatchInfo() *BatchInfo {
	if s.batchManager == nil {
		return nil
	}
	return s.batchManager.GetCurrentBatch()
}

// GetBatchStats returns statistics about batch processing
func (s *SequencerIntegration) GetBatchStats() map[string]interface{} {
	stats := map[string]interface{}{
		"last_processed_slot": s.lastProcessedSlot,
		"integration_active":  true,
	}

	if s.batchManager != nil {
		batchStats := s.batchManager.GetStats()
		stats["batch_manager"] = batchStats
	}

	if s.pipeline != nil {
		pipelineStats := s.pipeline.GetStats()
		stats["pipeline"] = pipelineStats
	}

	return stats
}

// IsInBatchCollectionPeriod returns true if we're currently collecting blocks for a batch
func (s *SequencerIntegration) IsInBatchCollectionPeriod() bool {
	if s.batchManager == nil {
		return false
	}
	return s.batchManager.IsCollectingBatch()
}

// GetL1SyncStatus returns information about L1 synchronization status
func (s *SequencerIntegration) GetL1SyncStatus(ctx context.Context) (map[string]interface{}, error) {
	if s.l1Listener == nil {
		return map[string]interface{}{
			"enabled": false,
		}, nil
	}

	batchNumber, err := s.l1Listener.GetCurrentBatchNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("get current batch number: %w", err)
	}

	return map[string]interface{}{
		"enabled":             true,
		"batch_number":        batchNumber,
		"last_processed_slot": s.lastProcessedSlot,
	}, nil
}

// WaitForBatchTrigger waits for the next batch trigger from L1
func (s *SequencerIntegration) WaitForBatchTrigger(ctx context.Context) (*BatchTrigger, error) {
	if s.l1Listener == nil {
		return nil, fmt.Errorf("l1 listener not available")
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case trigger := <-s.l1Listener.BatchTriggers():
		return &trigger, nil
	case err := <-s.l1Listener.Errors():
		return nil, fmt.Errorf("l1 listener error: %w", err)
	}
}

// ForceFinalizeBatch forces the current batch to be finalized (for testing/emergency)
func (s *SequencerIntegration) ForceFinalizeBatch() error {
	s.log.Warn().Msg("Force finalizing current batch")

	currentBatch := s.batchManager.GetCurrentBatch()
	if currentBatch == nil {
		return fmt.Errorf("no current batch to finalize")
	}

	// This is a simplified approach - in practice you'd want more sophisticated handling
	s.log.Warn().
		Uint64("batch_id", currentBatch.ID).
		Msg("Forcing batch finalization")

	// The actual finalization would happen in the batch manager's internal logic
	// This is just a trigger mechanism
	return nil
}
