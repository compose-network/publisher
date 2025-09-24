# Batch Synchronization Package

This package implements synchronized batch proving across sequencers as specified in the Superblock Construction Protocol (SBCP) and Settlement Layer documentation.

## Overview

The batch synchronization system ensures that:
1. All sequencers use Ethereum as a common clock for batch coordination
2. Batches are triggered every 10 Ethereum epochs (divisible epoch numbers)
3. Each batch contains a synchronized time window of L2 blocks
4. Proof generation follows the pipeline: Range → Aggregation → Network Aggregation
5. Completed proofs are submitted to the Shared Publisher (SP)

## Components

### L1 Listener (`listener.go`)
- Connects to Ethereum L1 via RPC
- Monitors for new epochs (every ~6.4 minutes)
- Triggers batch events when `epoch_number % batch_factor == 0`
- Provides epoch events and batch triggers via channels

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
cfg.SetL1RPC("https://mainnet.infura.io/v3/YOUR_KEY")

// Validate configuration
if err := cfg.Validate(); err != nil {
    log.Fatal("Invalid config:", err)
}

// Create components (assuming you have slot manager, collector, prover client)
l1Listener, err := batch.NewL1Listener(cfg.L1Listener, log)
if err != nil {
    log.Fatal("Failed to create L1 listener:", err)
}

batchManager, err := batch.NewManager(cfg.BatchManager, slotManager, l1Listener, log)
if err != nil {
    log.Fatal("Failed to create batch manager:", err)
}

pipeline, err := batch.NewPipeline(cfg.Pipeline, batchManager, collector, proverClient, log)
if err != nil {
    log.Fatal("Failed to create pipeline:", err)
}

// Start all components
ctx := context.Background()
go l1Listener.Start(ctx)
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
    l1Listener,
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
# L1 Configuration
export BATCH_L1_LISTENER_L1_RPC="https://mainnet.infura.io/v3/YOUR_KEY"
export BATCH_L1_LISTENER_BATCH_FACTOR=10
export BATCH_L1_LISTENER_POLL_INTERVAL=12s

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

  l1_listener:
    l1_rpc: "https://mainnet.infura.io/v3/YOUR_KEY"
    batch_factor: 10
    poll_interval: 12s

  batch_manager:
    chain_id: 1001
    max_batch_size: 320
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

1. **L1 Listener** detects epoch divisible by `batch_factor`
2. **Batch Manager** starts new batch, begins collecting blocks
3. **Sequencer Integration** reports produced blocks to batch manager
4. **Batch Manager** finalizes batch when next trigger occurs or size limit reached
5. **Pipeline** processes batch through proof generation stages
6. **Pipeline** submits completed proof to Shared Publisher

### Proof Generation Pipeline

1. **Range Proof**: op-succinct range program validates L2 block sequence
2. **Aggregation**: op-succinct aggregation program creates batch proof
3. **Network Aggregation**: superblock-prover creates final superblock proof
4. **Submission**: Final proof submitted to SP for L1 settlement

### Synchronization

- All sequencers listen to same L1 epochs
- Batch boundaries synchronized across all rollups
- 12-second block time aligned with Ethereum slots
- Batch factor of 10 epochs = ~64 minutes per batch

## Error Handling

- **L1 Connection Failures**: Automatic reconnection with exponential backoff
- **Proof Generation Failures**: Configurable retries with delays
- **Batch Timeouts**: Automatic batch finalization after timeout period
- **Resource Limits**: Concurrent job limits to prevent overload

## Metrics and Monitoring

Components expose metrics for:
- Batch collection rates
- Proof generation times
- Success/failure rates
- L1 synchronization status
- Resource utilization

## Testing

The package includes comprehensive test configurations:

```go
// Get test configuration
testCfg := batch.GetTestConfig(1001)

// Use with local test network
testCfg.L1Listener.L1RPC = "http://localhost:8545"
testCfg.L1Listener.BatchFactor = 2  // Faster batching
```

## Integration Points

### With Existing Systems

- **Slot Manager**: Uses existing 12-second slot timing
- **Proof Collector**: Extends existing op-succinct integration
- **Prover Client**: Uses existing superblock-prover HTTP client
- **Sequencer Coordinator**: Integrates with existing SBCP implementation

### With Shared Publisher

- Submits batch proofs via proof collector
- Coordinates with SP for superblock construction
- Provides batch metadata for L1 settlement

## Production Considerations

- Configure appropriate L1 RPC endpoints with high availability
- Set conservative timeouts for proof generation
- Monitor batch success rates and adjust parameters
- Implement alerting for batch failures
- Consider rate limiting for L1 API calls
- Ensure sufficient compute resources for proof generation

## Future Enhancements

- Dynamic batch sizing based on network conditions
- Improved error recovery and state reconstruction
- Integration with beacon chain for more accurate epoch tracking
- Support for multiple L1 networks
- Advanced proof caching and optimization
