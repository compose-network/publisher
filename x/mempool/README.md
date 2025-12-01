# x/mempool

Transaction lifecycle management for cross-chain coordinators.

## Overview

The `mempool` package implements pure protocol logic for managing cross-chain transactions through their lifecycle stages. It tracks transactions from staging through commitment to delivery, enforces ordering rules, and applies filtering based on the current sequencer state.

## Key Components

### Manager

The main coordinator that orchestrates all mempool operations:

```go
mgr := mempool.NewManager(mempool.DefaultConfig())

// Stage a transaction
mgr.StageTransaction(ctx, hash, xtID, kind, nonce, from, currentSlot)

// Get ordered transactions for block building
hashes, err := mgr.GetOrderedHashesForBlock(ctx, state, requestSeal, shouldHoldTx)

// Mark transactions as committed after block inclusion
mgr.MarkCommitted(ctx, slot, blockNumber, hashes)

// Mark transactions as delivered after finalization
mgr.MarkDelivered(ctx, hashes)
```

### Tracker

Maintains transaction records and provides efficient lookups:

- By hash
- By cross-chain transaction ID (xtID)
- By status (staged, committed)

### Ordering

Enforces transaction ordering rules:

1. All `putInbox` transactions before all `original` transactions
2. Within each category, transactions ordered by nonce
3. Transactions without xtID (standalone) come first

### Filter

Applies state-based filtering:

- During `StateSubmission`, only transactions in the `seal request` inclusion list are allowed
- Transactions without xtID always pass through
- Other states allow all transactions

### Policy

Enforces nonce gap rules:

- Transactions with unfillable nonce gaps are pruned after expiry
- Default expiry: 6 slots

## Transaction Lifecycle

```
StageTransaction
       ↓
  [StatusStaged]
       ↓
MarkCommitted
       ↓
 [StatusCommitted]
       ↓
MarkDelivered
       ↓
  [removed]
```

## Configuration

```go
config := mempool.Config{
    MaxFutureNonceGap:   1,  // Max nonce gap for single transaction
    NonceGapExpirySlots: 6,  // Slots before pruning gapped transactions
}

mgr := mempool.NewManager(config)
```

## Usage Example

```go
ctx := context.Background()
mgr := mempool.NewManager(mempool.DefaultConfig())

// Stage putInbox and original for a cross-chain transaction
mgr.StageTransaction(ctx, "putInboxHash", "xt1", mempool.KindPutInbox, 10, coordAddr, 1)
mgr.StageTransaction(ctx, "originalHash", "xt1", mempool.KindOriginal, 20, userAddr, 1)

// Get ordered hashes for block building (during BuildingFree state)
hashes, _ := mgr.GetOrderedHashesForBlock(
    ctx,
    mempool.StateBuildingFree,
    nil,
    nil,
)
// Returns: ["putInboxHash", "originalHash"]

// Mark committed after block inclusion
mgr.MarkCommitted(ctx, 2, 100, hashes)

// Mark delivered after finalization
mgr.MarkDelivered(ctx, hashes)
```

## Testing

Run tests:
```bash
go test ./x/mempool/
```

With coverage:
```bash
go test -cover ./x/mempool/
```

Current coverage: **91.6%**

## Design Principles

1. **Pure protocol logic**: No dependencies on execution layer (geth, etc.)
2. **Testable**: All components fully testable with mocks
3. **Type safety**: Uses string hashes and hex-encoded xtIDs (no execution-specific types)
4. **Thread-safe**: Tracker uses mutex for concurrent access
5. **Minimal**: Generic cross-chain transaction lifecycle management

## Integration

This module is designed to be used by `x/superblock/sequencer` to manage the transaction lifecycle. The sequencer coordinator uses the mempool manager instead of maintaining its own transaction tracking logic.

See the SDK modular architecture document for integration details.
