package batch

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs/collector"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs/prover"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/sequencer"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/slot"
)

// IntegrationExample shows how to integrate batch sync with existing sequencer
func IntegrationExample(chainID uint32, l1RPC, beaconRPC string, log zerolog.Logger) error {
	ctx := context.Background()

	// 1. Create configuration
	cfg := DefaultConfig()
	cfg.SetChainID(chainID)
	cfg.SetL1RPC(l1RPC)
	cfg.L1Listener.BeaconRPC = beaconRPC

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// 2. Create existing components (you'll have these already)
	slotManager := slot.NewManager(time.Now(), 12*time.Second, 2.0/3.0)

	// These would be your existing components
	var existingSequencerCoordinator sequencer.Coordinator
	var existingProofCollector collector.Service
	var existingProverClient *prover.HTTPClient

	// 3. Create batch components
	l1Listener, err := NewL1Listener(cfg.L1Listener, log)
	if err != nil {
		return fmt.Errorf("create L1 listener: %w", err)
	}

	batchManager, err := NewManager(cfg.BatchManager, slotManager, l1Listener, log)
	if err != nil {
		return fmt.Errorf("create batch manager: %w", err)
	}

	pipeline, err := NewPipeline(cfg.Pipeline, batchManager, existingProofCollector, existingProverClient, log)
	if err != nil {
		return fmt.Errorf("create pipeline: %w", err)
	}

	// 4. Create sequencer integration
	integration, err := NewSequencerIntegration(
		cfg.Integration,
		existingSequencerCoordinator,
		batchManager,
		pipeline,
		l1Listener,
		log,
	)
	if err != nil {
		return fmt.Errorf("create integration: %w", err)
	}

	// 5. Start all components
	log.Info().Msg("Starting batch synchronization system")

	if err := l1Listener.Start(ctx); err != nil {
		return fmt.Errorf("start L1 listener: %w", err)
	}

	if err := batchManager.Start(ctx); err != nil {
		return fmt.Errorf("start batch manager: %w", err)
	}

	if err := pipeline.Start(ctx); err != nil {
		return fmt.Errorf("start pipeline: %w", err)
	}

	if err := integration.Start(ctx); err != nil {
		return fmt.Errorf("start integration: %w", err)
	}

	log.Info().Msg("Batch synchronization system started successfully")

	// 6. Example of how to use the integration in your sequencer

	// When your sequencer produces a block:
	// integration.ReportBlock(slotNum, blockNum, blockHash, txCount, includedXTxs)

	// Check if we're in a batch collection period:
	// if integration.IsInBatchCollectionPeriod() {
	//     log.Info().Msg("Currently collecting blocks for batch")
	// }

	// Monitor batch events:
	go func() {
		for event := range batchManager.Events() {
			log.Info().
				Str("event_type", event.Type).
				Uint64("batch_id", event.BatchID).
				Msg("Batch event received")
		}
	}()

	// Monitor pipeline events:
	go func() {
		for event := range pipeline.GetJobEvents() {
			log.Info().
				Str("event_type", event.Type).
				Str("job_id", event.JobID).
				Str("stage", string(event.Stage)).
				Msg("Pipeline event received")
		}
	}()

	return nil
}

// ExtendExistingSequencer shows how to extend your existing sequencer bootstrap
func ExtendExistingSequencer(
	existingSequencerCoordinator sequencer.Coordinator,
	chainID uint32,
	l1RPC, beaconRPC string,
	log zerolog.Logger,
) (*SequencerIntegration, error) {

	// Create batch configuration
	cfg := DefaultConfig()
	cfg.SetChainID(chainID)
	cfg.SetL1RPC(l1RPC)
	cfg.L1Listener.BeaconRPC = beaconRPC

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid batch configuration: %w", err)
	}

	// You'll need to get these from your existing setup
	slotManager := slot.NewManager(time.Now(), 12*time.Second, 2.0/3.0)

	// Create batch components
	l1Listener, err := NewL1Listener(cfg.L1Listener, log)
	if err != nil {
		return nil, fmt.Errorf("create L1 listener: %w", err)
	}

	batchManager, err := NewManager(cfg.BatchManager, slotManager, l1Listener, log)
	if err != nil {
		return nil, fmt.Errorf("create batch manager: %w", err)
	}

	// You'll need to provide your existing proof infrastructure
	var existingProofCollector collector.Service
	var existingProverClient *prover.HTTPClient

	pipeline, err := NewPipeline(cfg.Pipeline, batchManager, existingProofCollector, existingProverClient, log)
	if err != nil {
		return nil, fmt.Errorf("create pipeline: %w", err)
	}

	// Create integration
	integration, err := NewSequencerIntegration(
		cfg.Integration,
		existingSequencerCoordinator,
		batchManager,
		pipeline,
		l1Listener,
		log,
	)
	if err != nil {
		return nil, fmt.Errorf("create integration: %w", err)
	}

	// Start components
	ctx := context.Background()
	go l1Listener.Start(ctx)
	go batchManager.Start(ctx)
	go pipeline.Start(ctx)
	go integration.Start(ctx)

	return integration, nil
}

// ProductionSetup shows production-ready configuration
func ProductionSetup(chainID uint32, l1RPC, beaconRPC string) Config {
	cfg := GetRecommendedProductionConfig(chainID, l1RPC)
	cfg.L1Listener.BeaconRPC = beaconRPC

	// Production optimizations
	cfg.L1Listener.PollInterval = 12 * time.Second    // Ethereum slot time
	cfg.BatchManager.BatchTimeout = 120 * time.Minute // 2 hour timeout
	cfg.Pipeline.MaxConcurrentJobs = 3                // Conservative for production
	cfg.Pipeline.JobTimeout = 45 * time.Minute        // Generous timeout

	return cfg
}

// TestSetup shows test configuration
func TestSetup(chainID uint32) Config {
	cfg := GetTestConfig(chainID)
	cfg.L1Listener.BeaconRPC = "http://localhost:5052" // Local beacon node

	// Fast settings for testing
	cfg.L1Listener.BatchFactor = 2 // Every 2 epochs
	cfg.L1Listener.PollInterval = 2 * time.Second
	cfg.BatchManager.MaxBatchSize = 5 // Small batches
	cfg.Pipeline.MaxConcurrentJobs = 1

	return cfg
}
