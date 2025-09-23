# Root README.md


The Rollup Shared Publisher is a decentralized coordination layer for cross-chain atomic transaction execution across
EVM-compatible rollups. It implements a two-phase commit (2PC) protocol to ensure transactions are either all committed
or all aborted across participating chains.

**WARNING**: This project is in active development. Breaking changes may occur.

**Note**: Requires Go 1.24+ for building applications.

## Quick Start

### Running the Shared Publisher (Leader)

```bash
# Build
make build

# Run with default config
./bin/rollup-shared-publisher

# Run with custom config
./bin/rollup-shared-publisher --config shared-publisher-leader-app/configs/config.yaml
```

### Implementing a Sequencer (Follower)

See [Sequencer Implementation Guide](#sequencer-implementation-guide) below.

## Architecture

The system uses a leader-follower model with the Shared Publisher coordinating cross-chain transactions:

```
┌─────────────────────────┐
│   Shared Publisher      │
│      (Leader)           │
└───────────┬─────────────┘
            │ 2PC Protocol
    ┌───────┴───────┐
    │               │
┌───▼──────┐   ┌────▼────┐
│Rollup A  │   │Rollup B │
│Sequencer │   │Sequencer│
└──────────┘   └─────────┘
```

## Modules

The Rollup Shared Publisher maintains several production-grade modules. See [x/README.md](./x/README.md) for detailed
documentation.

### Core Modules

* [Publisher](./x/publisher/README.md) - Central coordinator for 2PC protocol
* [Consensus](./x/consensus/README.md) - Two-phase commit implementation
* [Transport](./x/transport/README.md) - High-performance TCP networking layer

### Supporting Modules

* [Adapter](./x/adapter/README.md) - Interface for rollup integration
* [Auth](./x/auth/README.md) - ECDSA-based authentication
* [Codec](./x/codec/README.md) - Protobuf message encoding/decoding

## Sequencer Implementation Guide

To integrate your rollup as a sequencer (follower) in the system:

### 1. Install the SDK

```bash
go get github.com/ssvlabs/rollup-shared-publisher
```

### 2. Implement the Adapter Interface

```go
package sequencer

import (
	"context"
	"encoding/hex"

	"github.com/ssvlabs/rollup-shared-publisher/x/adapter"
	pb "github.com/ssvlabs/rollup-shared-publisher/proto/rollup/v1"
	"github.com/ssvlabs/rollup-shared-publisher/x/transport/tcp"
)

type MySequencerAdapter struct {
	adapter.BaseAdapter

	client  transport.Client
	chainID []byte

	// Your rollup-specific fields
	txPool   *TxPool
	executor *Executor
	mailbox  *Mailbox
}

func NewSequencerAdapter(chainID string, txPool *TxPool) *MySequencerAdapter {
	chainIDBytes, _ := hex.DecodeString(chainID)

	return &MySequencerAdapter{
		BaseAdapter: *adapter.NewBaseAdapter("my-rollup", "1.0.0", chainID),
		chainID:     chainIDBytes,
		txPool:      txPool,
		executor:    NewExecutor(),
		mailbox:     NewMailbox(),
	}
}
```

### 3. Handle Protocol Messages

```go
// Handle incoming cross-chain transaction request
func (s *MySequencerAdapter) HandleXTRequest(ctx context.Context, from string, req *pb.XTRequest) error {
    xtID, _ := req.XtID()

    // Extract transactions for this chain
    for _, tx := range req.Transactions {
        if bytes.Equal(tx.ChainId, s.chainID) {
        // Start timer for timeout
        timer := time.AfterFunc(3*time.Minute, func () {
        s.sendVote(xtID, false) // Vote abort on timeout
        })

    // Simulate transaction
    result := s.executor.Simulate(tx.Transaction)

    if result.RequiresCIRC {
        // Wait for CIRC messages from other chains
        s.waitForCIRC(xtID, result.Dependencies)
    } else {
        // Send vote immediately
        s.sendVote(xtID, result.Success)
    }

    timer.Stop()
        }
    }
    return nil
}

// Handle 2PC decision from publisher
func (s *MySequencerAdapter) HandleDecision(ctx context.Context, from string, decision *pb.Decided) error {
    if decision.Decision {
    // Commit: Include transaction in next block
        s.txPool.AddCrossChainTx(decision.XtId)
    } else {
    // Abort: Remove from pending
        s.txPool.RemovePending(decision.XtId)
    }
    return nil
}

// Send vote to publisher
func (s *MySequencerAdapter) sendVote(xtID *pb.XtID, vote bool) error {
    msg := &pb.Message{
        SenderId: s.client.GetID(),
        Payload: &pb.Message_Vote{
            Vote: &pb.Vote{
            SenderChainId: s.chainID,
            XtId:          xtID,
            Vote:          vote,
            },
        },
    }

    return s.client.Send(context.Background(), msg)
}
```

### 4. Connect to Shared Publisher

```go
func (s *MySequencerAdapter) Start(ctx context.Context) error {
    // Create TCP client with optional authentication
    config := tcp.DefaultClientConfig()
    config.ServerAddr = "publisher.example.com:8080"

    s.client = tcp.NewClient(config, log)

    // Optional: Add ECDSA authentication
    if privateKey != nil {
        authManager := auth.NewManager(privateKey)
        s.client = s.client.WithAuth(authManager)
    }

    // Set message handler
    s.client.SetHandler(s.handleMessage)

    // Connect with retry
    return s.client.ConnectWithRetry(ctx, "", 5)
}

func (s *MySequencerAdapter) handleMessage(ctx context.Context, from string, msg *pb.Message) error {
    switch payload := msg.Payload.(type) {
    case *pb.Message_XtRequest:
        return s.HandleXTRequest(ctx, from, payload.XtRequest)
    case *pb.Message_Decided:
        return s.HandleDecision(ctx, from, payload.Decided)
    case *pb.Message_CircMessage:
        return s.HandleCIRC(ctx, from, payload.CircMessage)
    default:
        return fmt.Errorf("unknown message type: %T", payload)
    }
}
```

### 5. Submit Blocks

```go
func (s *MySequencerAdapter) SubmitBlock(ctx context.Context, block *types.Block) error {
    // Get included cross-chain transactions
    includedXTs := s.txPool.GetIncludedCrossChainTxs(block)

    xtIDs := make([]*pb.XtID, len(includedXTs))
    for i, xt := range includedXTs {
        xtIDs[i] = xt.ID
    }

    // Submit block to publisher
    msg := &pb.Message{
        Payload: &pb.Message_Block{
        Block: &pb.Block{
        ChainId:       s.chainID,
        BlockData:     block.Encode(),
        IncludedXtIds: xtIDs,
        },
        },
    }

return s.client.Send(ctx, msg)
}
```

### 6. Handle CIRC Messages (Optional)

For rollups supporting inter-rollup communication:

```go
func (s *MySequencerAdapter) HandleCIRC(ctx context.Context, from string, circ *pb.CIRCMessage) error {
    // Add CIRC message to mailbox
    s.mailbox.AddMessage(circ)

    // Resume transaction simulation if waiting
    if waiter := s.getWaiter(circ.XtId); waiter != nil {
        waiter.Resume()
    }

return nil
}
```

## Configuration

### Publisher Configuration

```yaml
server:
  listen_addr: ":8080"
  max_connections: 1000
  read_timeout: 30s
  write_timeout: 30s
  max_message_size: 10485760  # 10MB

consensus:
  timeout: 60s

metrics:
  enabled: true
  port: 8081
```

### Sequencer Configuration

```go
config := tcp.ClientConfig{
    ServerAddr:      "publisher.example.com:8080",
    ConnectTimeout:  10 * time.Second,
    ReadTimeout:     30 * time.Second,
    WriteTimeout:    10 * time.Second,
    ReconnectDelay:  5 * time.Second,
    MaxMessageSize:  10 * 1024 * 1024,
    KeepAlive:       true,
    KeepAlivePeriod: 30 * time.Second,
}
```

## Monitoring

### Endpoints

- **Metrics**: `http://localhost:8081/metrics`
- **Health**: `http://localhost:8081/health`
- **Ready**: `http://localhost:8081/ready`
- **Stats**: `http://localhost:8081/stats`

### Key Metrics

```
publisher_consensus_active_transactions
publisher_consensus_transactions_total{state="commit|abort"}
publisher_consensus_duration_seconds
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

See [Superblock Construction Protocol](./docs/superblock_construction_protocol.md) for detailed protocol specification.

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
