# adapter

The adapter module provides interfaces and base implementations for integrating different rollup sequencer
implementations with the Publisher SDK.

## Overview

The adapter pattern allows different rollup technologies to implement their
specific logic while maintaining compatibility with the shared publisher's 2PC protocol.

## Architecture

```
adapter/
├── interfaces.go    # Core adapter interfaces
├── base.go         # Base implementation with defaults
└── README.md
```

## Interfaces

### SequencerAdapter

The main interface that rollup sequencers must implement:

```go
type SequencerAdapter interface {
// Identity
Name() string
ChainID() string

// Message handling for sequencers
OnXTRequest(ctx context.Context, req *pb.XTRequest) error
OnDecision(ctx context.Context, decision *pb.Decided) error

// Sequencer actions
SendVote(ctx context.Context, xtID *pb.XtID, vote bool) error
SubmitBlock(ctx context.Context, block *pb.Block) error

// Lifecycle
Start(ctx context.Context) error
Stop(ctx context.Context) error
}
```

### BaseAdapter

Provides default implementations for common functionality:

```go
type BaseAdapter struct {
name    string
version string
chainID string
}
```
