# Batch Synchronization Package

This package implements synchronized batch proving across sequencers.

## Overview

The batch synchronization system ensures that:
1. All sequencers use Ethereum as a common clock for batch coordination
2. Batches are triggered every 10 Ethereum epochs (divisible epoch numbers)
3. Each batch contains a synchronized time window of L2 blocks
4. Proof generation follows the pipeline: Range → Aggregation → Network Aggregation
5. Completed proofs are submitted to the Shared Publisher (SP)

## Components

### Epoch Tracker (`epoch_tracker.go`)
- **Time-based epoch calculation**
- Uses formula: `epoch = (time.Now() - genesisTime) / 12 seconds / 32 slots`
- Monitors for new epochs (every ~6.4 minutes)
- Triggers batch events when `epoch_number % batch_factor == 0`

### Batch Manager (`manager.go`)
- Manages batch lifecycle (collecting → proving → completed/failed)
- Coordinates with slot timing (12-second blocks)
- Tracks blocks added to current batch
- Enforces batch size limits and timeouts
- Emits batch lifecycle events

### Proof Pipeline (`pipeline.go`)
- Orchestrates proof generation workflow
- Integrates with existing `op-succinct` (collector) and `superblock-prover`
- Handles job queuing, processing, and retries
- Supports concurrent proof generation
- Submits completed proofs to Shared Publisher

### Sequencer Integration (`sequencer_integration.go`)
- Extends existing sequencer coordinator with batch awareness
- Reports produced blocks to batch manager
- Monitors batch events and coordinates accordingly
- Provides batch status information

### Configuration (`config.go`)
- Centralized configuration for all batch components
- Validation and defaults handling
- Production vs test configurations
- Environment-specific settings

## Usage

### Basic Setup

```go
import "github.com/ssvlabs/rollup-shared-publisher/x/superblock/batch"

// Create configuration
cfg := batch.DefaultConfig()
cfg.SetChainID(1001)
// Genesis time is already set to Ethereum Mainnet genesis in DefaultConfig()
// No RPC URLs needed!

// Validate configuration
if err := cfg.Validate(); err != nil {
    log.Fatal("Invalid config:", err)
}

// Create components
// IMPORTANT: Use the same genesis time for slot manager and epoch tracker
genesisTime := cfg.EpochTracker.GenesisTime
slotManager := slot.NewManager(genesisTime, 12*time.Second, 2.0/3.0)

epochTracker, err := batch.NewEpochTracker(cfg.EpochTracker, log)
if err != nil {
    log.Fatal("Failed to create epoch tracker:", err)
}

batchManager, err := batch.NewManager(cfg.BatchManager, slotManager, epochTracker, log)
if err != nil {
    log.Fatal("Failed to create batch manager:", err)
}

pipeline, err := batch.NewPipeline(cfg.Pipeline, batchManager, collector, proverClient, log)
if err != nil {
    log.Fatal("Failed to create pipeline:", err)
}

// Start all components
ctx := context.Background()
go epochTracker.Start(ctx)
go batchManager.Start(ctx)
go pipeline.Start(ctx)
```

### Integration with Sequencer

```go
// Create sequencer integration
integration, err := batch.NewSequencerIntegration(
    cfg.Integration,
    sequencerCoordinator,
    batchManager,
    pipeline,
    epochTracker,
    log,
)

// Start integration
go integration.Start(ctx)

// Report blocks as they're produced
integration.ReportBlock(slotNum, blockNum, blockHash, txCount, includedXTxs)

// Check batch status
if integration.IsInBatchCollectionPeriod() {
    log.Info("Currently collecting blocks for batch")
}
```

### Monitoring

All components provide statistics and events:

```go
// Get batch manager stats
batchStats := batchManager.GetStats()
log.Info("Batch stats:", batchStats)

// Monitor batch events
go func() {
    for event := range batchManager.Events() {
        log.Info("Batch event:", event.Type, event.BatchID)
    }
}()

// Monitor pipeline events
go func() {
    for event := range pipeline.GetJobEvents() {
        log.Info("Pipeline event:", event.Type, event.JobID, event.Stage)
    }
}()
```

## Configuration

### Environment Variables

Configuration supports environment variable overrides:

```bash
# Epoch Tracker Configuration
export BATCH_EPOCH_TRACKER_GENESIS_TIME="2020-12-01T12:00:23Z"  # Ethereum Mainnet genesis
export BATCH_EPOCH_TRACKER_BATCH_FACTOR=10
export BATCH_EPOCH_TRACKER_POLL_INTERVAL=12s

# Batch Manager
export BATCH_BATCH_MANAGER_CHAIN_ID=1001
export BATCH_BATCH_MANAGER_MAX_BATCH_SIZE=320
export BATCH_BATCH_MANAGER_BATCH_TIMEOUT=90m

# Pipeline
export BATCH_PIPELINE_MAX_CONCURRENT_JOBS=5
export BATCH_PIPELINE_JOB_TIMEOUT=30m
export BATCH_PIPELINE_MAX_RETRIES=3
```

### YAML Configuration

```yaml
batch:
  enabled: true

  epoch_tracker:
    genesis_time: "2020-12-01T12:00:23Z"  # Ethereum Mainnet genesis
    batch_factor: 10                       # Trigger batch every 10 epochs (spec requirement)
    poll_interval: 12s                     # Match Ethereum slot time

  batch_manager:
    chain_id: 1001
    max_batch_size: 320                    # 10 epochs * 32 slots
    batch_timeout: 90m

  pipeline:
    max_concurrent_jobs: 5
    job_timeout: 30m
    max_retries: 3
    retry_delay: 5m

  integration:
    chain_id: 1001
    enable_batch_sync: true
    block_reporting: true
```

## Architecture

### Batch Lifecycle

1. **Epoch Tracker** calculates current epoch from time (no RPC calls)
2. **Epoch Tracker** detects when `epoch % batch_factor == 0`
3. **Batch Manager** finalizes previous batch and starts new batch
4. **Sequencer Integration** reports produced blocks to batch manager
5. **Pipeline** processes completed batch through proof generation stages
6. **Pipeline** submits completed proof to Shared Publisher
