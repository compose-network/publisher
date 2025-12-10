package protocol

import (
	"context"

	pb "github.com/compose-network/specs/compose/proto"
)

// Handler defines the interface for SBCP protocol message handling
type Handler interface {
	// Handle processes SBCP protocol messages
	Handle(ctx context.Context, from string, msg *pb.Message) error

	// CanHandle returns true if this handler can process the message
	CanHandle(msg *pb.Message) bool

	// GetProtocolName returns the protocol name for logging/debugging
	GetProtocolName() string
}

// MessageHandler defines handlers for specific SBCP message types
type MessageHandler interface {
	HandleStartPeriod(ctx context.Context, from string, startPeriod *pb.StartPeriod) error
	HandleStartInstance(ctx context.Context, from string, startInstance *pb.StartInstance) error
	HandleRollback(ctx context.Context, from string, rb *pb.Rollback) error
}

// Validator defines message validation interface
type Validator interface {
	ValidateStartPeriod(startPeriod *pb.StartPeriod) error
	ValidateStartInstance(startSC *pb.StartInstance) error
	ValidateRollback(rb *pb.Rollback) error
}
