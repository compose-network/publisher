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
