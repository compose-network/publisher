# POC Shared Publisher - Phase 2

A proof-of-concept implementation of a shared publisher for cross-chain transaction coordination. This is **Phase 2** -
a stateful coordinator that implements a **Two-Phase Commit (2PC)** protocol to ensure atomic transactions across
multiple rollups.

## Architecture (Phase 2)

The system has evolved from a simple message relay to a central coordinator. The Shared Publisher (SP) now acts as the
leader in the 2PC protocol, managing the lifecycle of each cross-chain transaction (`XTRequest`).

```
┌───────────┐          ┌─────────────────┐          ┌───────────┐
│ Sequencer │          │     Shared      │          │ Sequencer │
│     A     │          │    Publisher    │          │     B     │
└───────────┘          └─────────────────┘          └───────────┘
      │                      │                          │
      │ 1. XTRequest         │                          │
      ├─────────────────────>│                          │
      │                      │ 2. XTRequest (Broadcast) │
      │                      ├─────────────────────────>│
      │<─────────────────────┤                          │
      │                      │                          │
      │ 3. Vote(Commit)      │ 3. Vote(Commit)          │
      ├─────────────────────>│<─────────────────────────┤
      │                      │                          │
      │                      │ 4. Decided(Commit)       │
      │<─────────────────────├─────────────────────────>│
      │                      │                          │
      │ 5. Block(Includes xT)│ 5. Block(Includes xT)    │
      ├─────────────────────>│<─────────────────────────┤
      │                      │                          │
```

### Components

1. **Shared Publisher (SP)** - The central coordinator.
    - Initiates and manages the 2PC protocol for each `XTRequest`.
    - Collects votes from sequencers and broadcasts the final decision (Commit/Abort).
    - Receives blocks from sequencers to confirm transaction inclusion.
    - Exposes detailed metrics and health endpoints on port `8081`.

2. **Sequencers (A, B, ...)** - Participants in the 2PC protocol.
    - Submit `XTRequest` bundles to the SP.
    - Send `Vote` messages (Commit/Abort) to the SP.
    - Receive final `Decided` messages from the SP.
    - On a `Commit` decision, include the transaction in a block and send the `Block` to the SP.

## Message Flow (Phase 2 - 2PC)

1. **Transaction Submission**: A client or sequencer sends an `XTRequest` to the SP.
2. **Initiate 2PC & Broadcast**: The SP assigns a unique `xt_id` to the transaction, starts a 3-minute timer, and
   broadcasts the `XTRequest` to all connected sequencers.
3. **Voting**: Each participating sequencer simulates the transaction and sends a `Vote` message to the SP.
4. **Decision**: The SP collects votes. If any sequencer votes `false` or the timer expires, the SP decides to **Abort
   **. If all vote `true`, the SP decides to **Commit**. The SP broadcasts the final `Decided` message.
5. **Block Submission**: Upon receiving a `Commit` decision, sequencers include the transaction in their next block and
   send the `Block` message to the SP.

### Message Types

Based on the protobuf definition in `api/proto/messages.proto`:

```protobuf
// User request for a cross-chain transaction
message XTRequest {
  repeated TransactionRequest transactions = 1;
}

message TransactionRequest {
  bytes chain_id = 1;
  repeated bytes transaction = 2;
}

// 2PC Vote message from a sequencer to the SP
message Vote {
  bytes sender_chain_id = 1;  // Which chain is voting
  uint32 xt_id = 2;           // Transaction ID
  bool vote = 3;              // true = Commit, false = Abort
}

// 2PC Decision message from the SP to sequencers
message Decided {
  uint32 xt_id = 1;           // Transaction ID
  bool decision = 2;          // true = Commit, false = Abort
}

// Block submission from a sequencer to the SP
message Block {
  bytes chain_id = 1;         // Which chain's block
  bytes block_data = 2;       // The actual block data
  repeated uint32 included_xt_ids = 3; // Which xTs are included
}

// Wrapper for all messages sent over the wire
message Message {
  string sender_id = 1; // Connection ID of the sender
  oneof payload {
    XTRequest xt_request = 2;
    Vote vote = 3;
    Decided decided = 4;
    Block block = 5;
  }
}
```

## Quick Start

### Prerequisites

- Go 1.24+
- Docker and Docker Compose
- Make

### Running with Docker

```bash
# Build and run the system
make docker-run

# Or manually
docker-compose up --build
```

### Running Locally

```bash
# Build the application
make build

# Run the publisher
make run

# Or directly
./bin/rollup-shared-publisher -config configs/config.yaml
```

### Testing the System

Use the provided Python scripts to simulate sequencers:

```bash
# Terminal 1: Start the publisher
make docker-run

# Terminal 2: Send a test transaction
python3 scripts/send_request.py

# Terminal 3: Run multiple clients simulation
python3 scripts/multiple_clients.py
```

## Configuration

The system uses a YAML configuration file (`configs/config.yaml`):

```yaml
server:
  listen_addr: ":8080"          # TCP port for sequencer connections
  write_timeout: 30s            # Connection write timeout
  max_message_size: 10485760    # 10MB max message size
  max_connections: 10           # Max concurrent connections (Phase 1)

metrics:
  enabled: true                 # Enable Prometheus metrics
  port: 8081                    # HTTP port for metrics

log:
  level: info                   # Log level
  pretty: false                 # JSON logging for Loki
  output: stdout                # Output to stdout (Loki integration)
```

### Environment Variables

All configuration values can be overridden using environment variables:

```bash
# Server configuration
export SERVER_LISTEN_ADDR=":9090"
export SERVER_MAX_CONNECTIONS=200

# Metrics configuration
export METRICS_PORT=3000
export METRICS_ENABLED=false

# Logging configuration
export LOG_LEVEL=debug
export LOG_PRETTY=true
```

**Pattern**: `<SECTION>_<KEY>` (dots replaced with underscores, all uppercase)

**Examples**:

- `server.listen_addr` → `SERVER_LISTEN_ADDR`
- `metrics.port` → `METRICS_PORT`
- `log.level` → `LOG_LEVEL`

**Priority**: ENV variables > YAML config > Default values

## Monitoring

### Metrics

Prometheus metrics are exposed on `http://localhost:8081/metrics`. In addition to the base-level metrics, Phase 2
introduces a rich set of metrics for the consensus protocol.

**New Consensus Metrics (`publisher_consensus_*`)**

- `publisher_consensus_transactions_total`: Total 2PC transactions, labeled by final state (`initiated`, `commit`,
  `abort`, `timeout`).
- `publisher_consensus_active_transactions`: A gauge showing the number of 2PC transactions currently in progress.
- `publisher_consensus_duration_seconds`: A histogram of the time it takes for a 2PC transaction to complete, labeled by
  state.
- `publisher_consensus_votes_received_total`: Total votes received, labeled by `chain_id` and `vote` (commit/abort).
- `publisher_consensus_vote_latency_seconds`: A histogram of the time from transaction start to vote reception.
- `publisher_consensus_timeouts_total`: A counter for the total number of transactions that timed out.
- `publisher_consensus_participants_per_transaction`: A histogram of how many chains participate in each transaction.
- `publisher_consensus_decisions_broadcast_total`: Total decisions broadcast, labeled by `decision` (commit/abort).

### Health Checks

- **Health**: `http://localhost:8081/health` - System health status
- **Ready**: `http://localhost:8081/ready` - Readiness status (has connections)
- **Stats**: `http://localhost:8081/stats` - Publisher statistics, including active 2PC transactions
- **Connections**: `http://localhost:8081/connections` - Active connections info

### Prometheus Setup

Use the provided Prometheus configuration:

```bash
# The config is optimized for Phase 1 development
cat monitoring/prometheus/prometheus.yml
```

## Development

### Building

```bash
# Build binary
make build

# Run tests
make test

# Run tests with coverage
make coverage

# Run linters
make lint

# Generate protobuf files
make proto
```

### Project Structure

```
├── api/proto/              # Protobuf definitions
├── cmd/publisher/          # Main application entry point
├── configs/               # Configuration files
├── internal/
│   ├── config/           # Configuration management
│   ├── consensus/        # Core 2PC coordinator logic and state management
│   ├── network/          # TCP server/client implementation
│   ├── proto/            # Generated protobuf files
│   └── publisher/        # Core publisher logic, message handlers, and integration
├── monitoring/           # Prometheus configuration
├── pkg/
│   ├── logger/          # Logging utilities
│   └── metrics/         # Prometheus metrics
└── scripts/             # Development and testing scripts
```

## Communication Protocol

The publisher and sequencers communicate over a custom TCP-based protocol designed for high performance and low
overhead. It does **not** use HTTP or gRPC. Clients must implement the following protocol to connect and interact with
the publisher.

### Protocol Design

The protocol is built on two core concepts:

1. **Persistent TCP Connections**: Clients establish a long-lived TCP connection to the publisher. This avoids the
   overhead of repeated handshakes (like in HTTP) and is ideal for the frequent, low-latency communication required
   between sequencers.

2. **Length-Prefixed Message Framing**: TCP is a stream-oriented protocol, meaning it does not have a built-in concept
   of message boundaries. To solve this, we implement a message framing strategy. Each Protobuf message is prefixed with
   a 4-byte header that specifies the exact length of the message that follows.

This design ensures that the receiver can reliably read complete messages from the stream without corruption or
ambiguity.

### Message Format

Every message sent over the TCP socket **must** adhere to the following binary format:

```
[ 4-byte Header | Protobuf Message Payload ]
```

* **Header (`[4-byte-length]`)**:
    * **Size**: 4 bytes (32 bits).
    * **Content**: An unsigned integer representing the size of the *Protobuf Message Payload* in bytes.
    * **Encoding**: Big Endian byte order.

* **Protobuf Message Payload**:
    * **Content**: The binary data resulting from serializing a `Message` struct (defined in `api/proto/messages.proto`)
      using the Protocol Buffers library.

### How to Connect and Send a Request

A client (sequencer) implementation must perform the following steps:

1. **Establish Connection**: Open a standard TCP socket to the publisher's listen address (e.g., `localhost:8080`).

2. **Construct Message**: Create an instance of the `XTRequest` message and populate it with the necessary transaction
   data. Wrap this `XTRequest` inside the top-level `Message` object.

3. **Serialize**: Use the Protobuf library for your language to serialize the `Message` object into a byte array.

4. **Frame and Send**:
   a. Get the length of the serialized byte array from the previous step.
   b. Create a 4-byte buffer containing this length, encoded as a Big Endian `uint32`.
   c. Write the 4-byte length header to the TCP socket.
   d. Immediately after, write the serialized message byte array to the socket.
