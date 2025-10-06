# Batch Synchronization Package

This package implements synchronized batch proving across sequencers.

## Overview

The batch synchronization system ensures that:
1. All sequencers use Ethereum as a common clock for batch coordination
2. Batches are triggered every 10 Ethereum epochs (divisible epoch numbers)
3. Each batch contains a synchronized time window of L2 blocks
4. Proof generation follows the pipeline: Range → Aggregation → Network Aggregation
5. Completed proofs are submitted to the Shared Publisher (SP)

## Package Structure

```
batch/
├── config.go                   # Simplified batch configuration (3 core fields)
├── spec.go                     # Protocol specification constants (immutable)
├── constants.go                # Implementation defaults (overridable)
├── epoch_tracker.go            # Time-based epoch calculation
├── manager.go                  # Batch lifecycle management
├── pipeline.go                 # Proof generation orchestration
├── sequencer_integration.go    # Sequencer coordinator integration
└── types/                      # Domain types
    ├── batch.go                # Batch domain types
    └── pipeline.go             # Pipeline domain types
```

## Components

### Epoch Tracker (`epoch_tracker.go`)
- **Time-based epoch calculation** (no RPC required)
- Uses formula: `epoch = (time.Now() - genesisTime) / 12 seconds / 32 slots`
- Monitors for new epochs (every ~6.4 minutes)
- Triggers batch events when `epoch_number % batch_factor == 0`
- Configuration in `EpochTrackerConfig`

### Batch Manager (`manager.go`)
- Manages batch lifecycle states: `collecting → proving → completed/failed`
- Coordinates with slot timing (12-second blocks)
- Tracks blocks added to current batch
- Enforces batch size limits and timeouts
- Emits batch lifecycle events
- Configuration in `ManagerConfig`

### Proof Pipeline (`pipeline.go`)
- Orchestrates proof generation workflow
- Pipeline stages: `idle → range_proof → aggregation → network_agg → completed/failed`
- Integrates with existing `op-succinct` (collector) and `superblock-prover`
- Handles job queuing, processing, and retries
- Supports concurrent proof generation
- Submits completed proofs to Shared Publisher
- Configuration in `PipelineConfig`

### Sequencer Integration (`sequencer_integration.go`)
- Extends existing sequencer coordinator with batch awareness
- Reports produced blocks to batch manager
- Monitors batch events and coordinates accordingly
- Provides batch status information
- Configuration in `IntegrationConfig`

### Types Package (`types/`)

Domain types are organized in a separate package following Go best practices:

**`types/batch.go`**
- `BatchState`: Lifecycle state enum (`collecting`, `proving`, `completed`, `failed`)
- `BatchInfo`: Comprehensive batch information
- `BatchBlockInfo`: L2 block within a batch
- `BatchEvent`: Batch lifecycle event
- `BatchTrigger`: Signal to start a new batch

**`types/pipeline.go`**
- `PipelineStage`: Proof generation stage enum (`idle`, `range_proof`, `aggregation`, `network_agg`, `completed`, `failed`)
- `PipelineJob`: Batch proof generation job
- `PipelineJobEvent`: Pipeline job event

### Configuration

Configuration types are colocated with their implementations:
- `Config` (main config) in `config.go`
- `ManagerConfig` in `manager.go`
- `PipelineConfig` in `pipeline.go`
- `IntegrationConfig` in `sequencer_integration.go`
- `EpochTrackerConfig` in `epoch_tracker.go`

## Usage

### Basic Setup

```go
import (
    "github.com/ssvlabs/rollup-shared-publisher/x/superblock/batch"
    "github.com/ssvlabs/rollup-shared-publisher/x/superblock/slot"
)

// Simplified configuration (only 3 core fields + 2 optional)
cfg := batch.Config{
    Enabled:             true,
    GenesisTime:         1606824023, // Ethereum Mainnet genesis (Unix timestamp)
    ChainID:             11155111,   // Sepolia
    MaxConcurrentJobs:   5,          // Optional: defaults to 5
    WorkerPollInterval:  10 * time.Second, // Optional: defaults to 10s
}

// Create slot manager (uses same genesis time)
genesisTime := time.Unix(cfg.GenesisTime, 0).UTC()
slotManager := slot.NewManager(genesisTime, 12*time.Second, 2.0/3.0)

// Create epoch tracker
epochTrackerCfg := batch.EpochTrackerConfig{
    GenesisTime: cfg.GenesisTime,
    BatchFactor: 10, // Trigger batch every 10 epochs
}
epochTracker, err := batch.NewEpochTracker(epochTrackerCfg, log)
if err != nil {
    log.Fatal("Failed to create epoch tracker:", err)
}

// Create batch manager
managerCfg := batch.ManagerConfig{
    ChainID:      cfg.ChainID,
    MaxBatchSize: 320, // 10 epochs * 32 slots
    BatchTimeout: 90 * time.Minute,
}
batchManager, err := batch.NewManager(managerCfg, slotManager, epochTracker, log)
if err != nil {
    log.Fatal("Failed to create batch manager:", err)
}

// Create proof pipeline
pipelineCfg := batch.PipelineConfig{
    MaxConcurrentJobs: cfg.MaxConcurrentJobs,
    JobTimeout:        30 * time.Minute,
    MaxRetries:        3,
    RetryDelay:        5 * time.Minute,
}
pipeline, err := batch.NewPipeline(pipelineCfg, batchManager, collector, proverClient, log)
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
integrationCfg := batch.IntegrationConfig{
    ChainID:         cfg.ChainID,
    EnableBatchSync: true,
    BlockReporting:  true,
}
integration, err := batch.NewSequencerIntegration(
    integrationCfg,
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

### Simplified YAML Configuration

The batch config has been drastically simplified to only essential fields:

```yaml
batch:
  enabled: true
  genesis_time: 1606824023           # Unix timestamp (Ethereum Mainnet genesis)
  chain_id: 11155111                 # Your chain ID (e.g., Sepolia)
  max_concurrent_jobs: 5             # Optional: pipeline concurrency (default: 5)
  worker_poll_interval: 10s          # Optional: worker polling (default: 10s)
```

### Environment Variables

Configuration supports environment variable overrides:

```bash
# Core configuration
export BATCH_ENABLED=true
export BATCH_GENESIS_TIME=1606824023     # Unix timestamp
export BATCH_CHAIN_ID=11155111

# Optional configuration
export BATCH_MAX_CONCURRENT_JOBS=5
export BATCH_WORKER_POLL_INTERVAL=10s
```

### Constants

The package uses three categories of constants:

**Specification Constants** (`spec.go`) - Immutable protocol values:
- `SlotDuration = 12s` (Ethereum consensus spec)
- `SlotsPerEpoch = 32` (Ethereum consensus spec)
- `BatchFactor = 10` (Settlement layer spec)
- `EthereumMainnetGenesis = 1606824023` (Unix timestamp)

**Implementation Defaults** (`constants.go`) - Overridable defaults:
- `DefaultMaxBatchSize = 320` (10 epochs * 32 slots)
- `DefaultBatchTimeout = 90m`
- `DefaultMaxConcurrentJobs = 5`
- `DefaultJobTimeout = 30m`
- `DefaultMaxRetries = 3`
- `DefaultRetryDelay = 5m`
- `DefaultWorkerPollInterval = 10s`

**Channel Buffers** (`constants.go`):
- `DefaultEpochEventChannelSize = 100`
- `DefaultBatchEventChannelSize = 100`
- `DefaultBatchTriggerChannelSize = 10`
- `DefaultErrorChannelSize = 50`

## Architecture

### Batch Lifecycle

1. **Epoch Tracker** calculates current epoch from time (no RPC calls)
2. **Epoch Tracker** detects when `epoch % batch_factor == 0`
3. **Batch Manager** finalizes previous batch and starts new batch
4. **Sequencer Integration** reports produced blocks to batch manager
5. **Pipeline** processes completed batch through proof generation stages
6. **Pipeline** submits completed proof to Shared Publisher
