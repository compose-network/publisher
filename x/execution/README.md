# x/execution

Minimal execution layer adapter interface for cross-chain coordinator.

## Overview

The `execution` package defines the adapter contract between cross-chain coordinator protocol logic (in `coordinator protocol`) and execution layer implementations (op-geth, Arbitrum Nitro, etc.).

**This is NOT a full execution layer abstraction**. It only exposes the specific operations cross-chain coordinator needs.

## Interface

```go
type Adapter interface {
    // Simulate transactions for voting
    SimulateTransactions(ctx, txs) (*SimulationResult, error)

    // Inject transactions into mempool
    InjectTransactions(ctx, hashes) error

    // Query nonces
    GetNonce(ctx, address) (uint64, error)

    // Retrieve transaction data
    GetTransaction(hash) (TxData, error)

    // Sign putInbox transactions
    SignPutInboxTransaction(tx) (signedTx, hash, error)
}
```

## TxData Format

Execution-agnostic transaction representation:

```go
type TxData struct {
    Hash      string
    From      []byte
    To        []byte
    Nonce     uint64
    Data      []byte
    Value     []byte
    Gas       uint64
    GasFeeCap []byte
    GasTipCap []byte
}
```

Implementations convert between `TxData` and execution-specific types (e.g., `*types.Transaction` for geth).

## Implementation

### op-geth

See `op-geth/eth/sbcp_adapter.go` for the geth implementation:

```go
// GethAdapter implements execution.Adapter for op-geth
type GethAdapter struct {
    backend *EthAPIBackend
    eth     *Ethereum
    signer  *signing.CoordinatorSigner
    chainID *big.Int
}
```

Initialized in `op-geth/eth/backend.go`:

```go
coordSigner, _ := signing.NewCoordinatorSigner(coordinatorKey, chainID)
gethAdapter := NewGethAdapter(apiBackend, eth, coordSigner)
```

## Design Principles

1. **Minimal scope** - Only what cross-chain coordinator needs, nothing more
2. **Type agnostic** - No execution-specific types leak into cross-chain coordinator
3. **Testable** - MockAdapter for unit tests
4. **Simple** - ~100 LOC vs. 500+ LOC for full Backend abstraction

## Non-Goals

- ❌ Abstract the entire execution layer
- ❌ Support smart contract deployment
- ❌ Provide full RPC functionality
- ❌ Handle consensus logic

## Usage

Implementations are execution-specific and live outside this package:

```
x/execution/              # Interface definition
op-geth/eth/sbcp_adapter.go    # Geth implementation
arbitrum-adapter/backend.go    # (Future) Arbitrum implementation
```

The cross-chain coordinator sequencer coordinator depends only on the `Adapter` interface, not on any specific implementation.
