# Publisher

The Publisher is a coordination layer for achieving synchronous composability and atomic execution of
transactions across multiple EVM-compatible rollups.

It implements the **Superblock Construction Protocol (SBCP)**, where a central publisher node orchestrates a slot-based
timeline for participating rollups. Sequencers follow this timeline to build their respective L2 blocks, which are then
aggregated into a "superblock" and published to L1. This process uses a two-phase commit (2PC) mechanism to ensure that
cross-chain transactions are atomically committed or aborted across all involved rollups.

**WARNING**: This project is in active development. Breaking changes may occur.

**Note**: Requires Go 1.24+ for building applications.

## Quick Start

### Running the Shared Publisher (Leader)

```bash
# Build
make build

# Run with default config
./bin/publisher

# Run with custom config
./bin/publisher --config publisher-leader-app/configs/config.yaml
```

### Implementing a Sequencer (Follower)

See [Sequencer Implementation Guide](#sequencer-implementation-guide) below.

## Architecture

The Publisher implements the **Superblock Construction Protocol (SBCP)**, a system designed to coordinate
the creation of "superblocks" that bundle transactions from multiple independent rollups. This enables synchronous
composability and atomic cross-chain transactions in a multi-rollup environment.

The protocol is orchestrated by a central node, the **Shared Publisher (SP)**, which manages a slot-based timeline (
aligned with Ethereum slots). Participating rollup **Sequencers** follow the SP's timeline to build their individual L2
blocks, which are then aggregated into a final superblock and published to L1.

```
┌──────────────────────────────────┐
│      Shared Publisher (Leader)   │
│ (SBCP Coordinator, 2PC Manager)  │
└─────────────────┬────────────────┘
                  │ SBCP Protocol (Slots, Seal Requests)
                  │ 2PC Protocol (Votes, Decisions)
          ┌───────┴───────────┐
          │                   │
┌─────────▼───────────┐   ┌───▼───────────────┐
│ Rollup A Sequencer  │   │ Rollup B Sequencer│
│ (Follower)          │   │ (Follower)        │
└─────────────────────┘   └───────────────────┘
```

The core logic is modular, separating the low-level 2PC consensus and networking from the high-level superblock
coordination protocol. See [x/README.md](./x/README.md) for a full overview of the available modules.

## Modules

The project is composed of several modules, located in the `x/` directory. See [x/README.md](./x/README.md) for detailed
documentation on each.

* **[Superblock](./x/superblock/README.md)**: Implements the Superblock Construction Protocol (SBCP), orchestrating
  slot-based block creation, cross-chain transaction coordination, and final superblock assembly for L1 publication.
* **[Publisher](./x/publisher/README.md)**: The central coordinator that manages sequencer connections and message
  broadcasting.
* **[Consensus](./x/consensus/README.md)**: The core two-phase commit (2PC) implementation for achieving atomic
  consensus on cross-chain transactions.
* **[Transport](./x/transport/README.md)**: A high-performance TCP networking layer for communication between the
  publisher and sequencers.
* **[Adapter](./x/adapter/README.md)**: Provides interfaces and base implementations for integrating rollup sequencers.
* **[Auth](./x/auth/README.md)**: Handles ECDSA-based message signing and verification.
* **[Codec](./x/codec/README.md)**: Manages Protobuf message encoding and decoding.

## Sequencer Integration

To integrate a rollup as a participant (a "follower"), a sequencer must connect to the Shared Publisher and handle the
messages defined by the Superblock Construction Protocol (SBCP).

The `x/adapter` module provides base interfaces, and `x/superblock/sequencer` contains a reference implementation and
state machine for a sequencer participating in SBCP. Developers should consult these modules for integration details.

## Configuration

The primary executable is the Shared Publisher leader application. For detailed configuration options, see the
application's README:

- **[publisher-leader-app/README.md](./publisher-leader-app/README.md)**

## Monitoring

The Shared Publisher exposes an HTTP API server (default port `:8081`) for monitoring and health checks.

### Endpoints

- **`GET /health`**: Liveness probe.
- **`GET /ready`**: Readiness probe (returns `503` until at least one sequencer is connected).
- **`GET /stats`**: Internal application statistics and build info.
- **`GET /metrics`**: Prometheus metrics endpoint.

### Key Metrics

A sample of key Prometheus metrics exposed by the publisher:

```
# Consensus Metrics
publisher_consensus_transactions_total{state="commit|abort"}
publisher_consensus_active_transactions
publisher_consensus_duration_seconds

# Transport Metrics
publisher_transport_connections_active
publisher_transport_messages_total{type,direction}
```

## Development

### Prerequisites

- Go 1.24+
- Docker & Docker Compose
- Protocol Buffers compiler

### Building

```bash
# Build all components
make build

# Run tests
make test

# Run with coverage
make coverage

# Lint code
make lint

# Generate protobuf
make proto-gen
```

### Running Locally

```bash
# Start publisher with docker-compose
docker-compose up -d

# Check logs
docker-compose logs -f publisher

# Stop
docker-compose down
```

## Protocol Specification

See [Superblock Construction Protocol](./spec/superblock_construction_protocol.md) for detailed protocol specification.

## Security

- **Authentication**: Optional ECDSA-based message signing
- **Authorization**: Trusted key management for known sequencers
- **Network**: TLS support (recommended for production)
- **Timeouts**: Configurable timeouts prevent indefinite blocking

## Contributing

Please read [CONTRIBUTING.md](./CONTRIBUTING.md) for details on our code of conduct and the process for submitting pull
requests.

## License

TODO
