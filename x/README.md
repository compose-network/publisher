# List of Modules

The Rollup Shared Publisher SDK provides production-grade modules for building cross-chain coordination systems. These
modules work together to enable atomic transaction execution across multiple rollups.

## Core Modules

Core modules provide essential functionality for the 2PC coordination protocol.

### Publisher Module

* [Publisher](./publisher/README.md) - Central coordinator implementing the leader role in 2PC protocol
    - Manages cross-chain transaction lifecycle
    - Broadcasts decisions to all participants
    - Handles connection management
    - Tracks active transactions and chain participation

### Consensus Module

* [Consensus](./consensus/README.md) - Two-phase commit protocol implementation
    - State management for active transactions
    - Vote collection and decision logic
    - Timeout handling for stuck transactions
    - CIRC message queue management
    - Metrics and monitoring

### Transport Module

* [Transport](./transport/README.md) - High-performance networking layer
    - TCP-based communication with zero-copy optimizations
    - Connection pooling and health monitoring
    - Automatic reconnection with exponential backoff
    - Optional authentication support
    - Worker pools for concurrent connection handling

## Supporting Modules

Supporting modules extend the capabilities of the core system.

### Adapter Module

* [Adapter](./adapter/README.md) - Integration interface for rollup sequencers
    - Base implementation with common functionality
    - Message routing and handling
    - Lifecycle hooks for initialization and cleanup
    - Extensible design for custom rollup logic

### Auth Module

* [Auth](./auth/README.md) - ECDSA-based authentication system
    - Message signing and verification
    - Trusted key management
    - Ethereum-compatible signatures
    - Optional authentication for secure environments

### Codec Module

* [Codec](./codec/README.md) - Message encoding and serialization
    - Protobuf-based wire format
    - Length-prefixed framing
    - Buffer pooling for performance
    - Extensible codec registry

## Module Architecture

```
┌─────────────────────────────────────────┐
│            Publisher Module             │
│  (Coordinator, Message Router, Stats)   │
└─────────────────┬───────────────────────┘
│
┌─────────┴─────────┐
│                   │
┌───────▼────────┐ ┌────────▼────────┐
│   Consensus    │ │    Transport    │
│   (2PC Logic)  │ │  (TCP Network)  │
└───────┬────────┘ └────────┬────────┘
│                   │
└─────────┬─────────┘
│
┌─────────────┼─────────────┐
│             │             │
┌───▼──┐    ┌────▼───┐    ┌────▼───┐
│ Auth │    │ Codec  │    │Adapter │
└──────┘    └────────┘    └────────┘
```

## Usage Patterns

### For Shared Publisher (Leader)

```go
import (
    "github.com/ssvlabs/rollup-shared-publisher/x/publisher"
    "github.com/ssvlabs/rollup-shared-publisher/x/consensus"
    "github.com/ssvlabs/rollup-shared-publisher/x/transport/tcp"
)

// Create coordinator
coordinator := consensus.New(logger, consensus.Config{
    Role:     consensus.Leader,
    Timeout:  60 * time.Second,
})

// Create transport
server := tcp.NewServer(transportConfig, log)

// Create publisher
pub := publisher.New(
    publisher.WithTransport(server),
    publisher.WithConsensus(coordinator),
)

// Start
pub.Start(ctx)
```

### For Sequencer (Follower)

```go
import (
    "github.com/ssvlabs/rollup-shared-publisher/x/adapter"
    "github.com/ssvlabs/rollup-shared-publisher/x/transport/tcp"
    "github.com/ssvlabs/rollup-shared-publisher/x/auth"
)

// Implement adapter
type MyAdapter struct {
    adapter.BaseAdapter
    // custom fields
}

// Create client
client := tcp.NewClient(config, log)

// Optional: Add authentication
if privateKey != nil {
    authManager := auth.NewManager(privateKey)
    client = client.WithAuth(authManager)
}

// Connect
client.Connect(ctx, publisherAddr)
```

## Performance Considerations

- **Zero-copy networking**: Buffer pools minimize allocations
- **Worker pools**: Concurrent connection handling
- **Batching**: Multiple transactions per message
- **Compression**: Optional message compression (planned)
- **Metrics**: Comprehensive monitoring for optimization

## Security Model

1. **Optional Authentication**: ECDSA signatures for trusted environments
2. **Message Integrity**: Cryptographic verification of messages
3. **Timeout Protection**: Configurable timeouts prevent blocking
4. **Connection Limits**: Maximum connection limits prevent DoS
5. **TLS Support**: Encryption for network communication (recommended)

## Module Development

To create a custom module:

1. Define interfaces in `interfaces.go`
2. Implement core logic
3. Add metrics collection
4. Write comprehensive tests
5. Document public APIs
