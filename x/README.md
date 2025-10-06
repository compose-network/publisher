# List of Modules

The Rollup Shared Publisher SDK provides production-grade modules for building cross-chain coordination systems. These
modules work together to enable atomic transaction execution across multiple rollups.

## Modules

The SDK is organized into several packages (`x/*`), which can be grouped into three main layers:

### 1. Protocol Layer

This layer implements the high-level coordination logic.

- **[Superblock](./superblock/README.md)**: Implements the Superblock Construction Protocol (SBCP) for slot-based,
  multi-rollup block creation and L1 publication. This is the primary protocol implementation.

### 2. Core Service Layer

This layer provides the fundamental building blocks for the coordination service.

- **[Publisher](./publisher/README.md)**: A basic coordinator that manages connections and broadcasts messages. It is
  wrapped by the `superblock` module to enable SBCP.
- **[Consensus](./consensus/README.md)**: A core implementation of the two-phase commit (2PC) protocol used for reaching
  agreement on cross-chain transactions.
- **[Transport](./transport/README.md)**: A high-performance TCP networking layer for communication between all nodes.

### 3. Supporting Modules

These modules provide cross-cutting concerns and utilities.

- **[Adapter](./adapter/README.md)**: Defines the interfaces for integrating a rollup sequencer with the publisher.
- **[Auth](./auth/README.md)**: An optional ECDSA-based authentication system for securing communication.
- **[Codec](./codec/README.md)**: Handles Protobuf-based message serialization and framing.

## Module Architecture

The architecture is layered, with the `superblock` module orchestrating the underlying services.

```
┌───────────────────────────────────────────┐
│           Superblock Module               │
│ (SBCP Coordinator, Slot Machine, Proving) │
└─────────────────┬─────────────────────────┘
                  │ Wraps
┌─────────────────▼───────────────────────┐
│            Publisher Module             │
│  (Message Router, Connection Manager)   │
└─────────────────┬───────────────────────┘
                  │ Uses
          ┌───────┴─────────┐
          │                 │
┌───────▼────────┐ ┌────────▼────────┐
│   Consensus    │ │    Transport    │
│   (2PC Logic)  │ │  (TCP Network)  │
└───────┬────────┘ └────────┬────────┘
        │                   │
        └─────────┬─────────┘
                  │
      ┌───────────┼─────────────┐
      │           │             │
┌─────▼─┐     ┌───▼────┐    ┌───▼────┐
│ Codec │     │  Auth  │    │ Adapter│
└───────┘     └────────┘    └────────┘
```

## Usage Patterns

The following patterns demonstrate how to set up the full Superblock Construction Protocol (SBCP).

### For Shared Publisher (Leader)

The leader node is created by wrapping a base `publisher` with the `sbadapter` to inject the SBCP coordination logic.

```go
import (
    "github.com/ssvlabs/rollup-shared-publisher/x/publisher"
    "github.com/ssvlabs/rollup-shared-publisher/x/consensus"
    "github.com/ssvlabs/rollup-shared-publisher/x/transport/tcp"
    "github.com/ssvlabs/rollup-shared-publisher/x/superblock"
    sbadapter "github.com/ssvlabs/rollup-shared-publisher/x/superblock/adapter"
)

// 1. Create base components
consensusCoord := consensus.New(log, consensus.Config{...})
tcpServer := tcp.NewServer(transportConfig, log)
basePublisher, _ := publisher.New(
    log,
	publisher.WithTransport(tcpServer),
    publisher.WithConsensus(consensusCoord),
	)

// 2. Define SBCP configuration
sbcpConfig := superblock.DefaultConfig()
// ... customize sbcpConfig.Slot, sbcpConfig.L1, sbcpConfig.Proofs ...

// 3. Wrap the base publisher to create the Superblock Publisher
// This injects the SBCP logic and requires dependencies for L1 and proofs.
superblockPublisher, err := sbadapter.WrapPublisher(
    basePublisher,
    sbcpConfig,
    log,
    consensusCoord,
    tcpServer,
    collectorSvc, // proofs.collector.Service
    proverClient, // proofs.ProverClient
)
if err != nil { ... }

// 4. Start the publisher
superblockPublisher.Start(ctx)
```

### For Sequencer (Follower)

The `x/superblock/sequencer/bootstrap` helper provides the quickest way to set up a sequencer for SBCP, including P2P
communication for CIRC messages.

```go
import (
    "github.com/ssvlabs/rollup-shared-publisher/x/superblock/sequencer/bootstrap"
)

// Use the bootstrap helper to set up a sequencer for SBCP
rt, err := bootstrap.Setup(ctx, bootstrap.Config{
    ChainID:   myChainIDBytes,
    SPAddr:    "shared-publisher.example.com:8080",
    PeerAddrs: map[string]string{
    "11155111": "sequencer-a.example.com:9000",
    "84532":    "sequencer-b.example.com:9000",
    },
    Log: log,
})
if err != nil { ... }

// Start connects to the SP and peers
rt.Start(ctx)

// The runtime's Coordinator can now be integrated with the sequencer's block production logic.
// For example, the sequencer would call these methods on the coordinator:
// rt.Coordinator.OnBlockBuildingStart(...)
// rt.Coordinator.OnBlockBuildingComplete(...)
```

## Performance Considerations

- **Zero-copy networking**: Buffer pools minimize allocations
- **Worker pools**: Concurrent connection handling
- **Batching**: Multiple transactions per message
- **Compression**: Optional message compression (planned)
- **Metrics**: Comprehensive monitoring for optimization

## Module Development

To create a custom module:

1. Define interfaces in `interfaces.go`
2. Implement core logic
3. Add metrics collection
4. Write comprehensive tests
5. Document public APIs
