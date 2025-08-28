package adapter

import (
	"context"

	pb "github.com/ssvlabs/rollup-shared-publisher/proto/rollup/v1"
)

// Adapter defines the interface for a rollup-specific implementation that
// integrates with the shared publisher. It provides methods for identifying
// the rollup and handling various types of messages received from the publisher.
type Adapter interface {
	// Name returns the unique identifier of the rollup implementation (e.g., "optimism").
	Name() string
	// Version returns the version string of the rollup implementation.
	Version() string
	// ChainID returns the blockchain identifier that this rollup instance
	// is configured to operate on.
	ChainID() string

	// HandleXTRequest processes an incoming cross-chain transaction request (XTRequest).
	// The 'from' parameter indicates the sender's identifier.
	HandleXTRequest(ctx context.Context, from string, req *pb.XTRequest) error

	// HandleVote processes an incoming 2PC vote message.
	// The 'from' parameter indicates the sender's identifier.
	HandleVote(ctx context.Context, from string, vote *pb.Vote) error

	// HandleDecision processes an incoming 2PC decision message (commit/abort).
	// The 'from' parameter indicates the sender's identifier.
	HandleDecision(ctx context.Context, from string, decision *pb.Decided) error

	// HandleBlock processes an incoming block submission.
	// The 'from' parameter indicates the sender's identifier.
	HandleBlock(ctx context.Context, from string, block *pb.Block) error

	// OnStart is a lifecycle hook called when the adapter is starting.
	// Implementations can use this for initialization logic.
	OnStart(ctx context.Context) error

	// OnStop is a lifecycle hook called when the adapter is stopping.
	// Implementations can use this for cleanup logic.
	OnStop(ctx context.Context) error
}
