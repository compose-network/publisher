package publisher

import (
	"context"
	"net/http"

	pb "github.com/ssvlabs/rollup-shared-publisher/proto/rollup/v1"
)

// Publisher interface defines the main publisher contract
type Publisher interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	GetStats() map[string]interface{}

	// MessageRouter returns the router for registering custom handlers
	MessageRouter() MessageRouter

	HandleMessage(ctx context.Context, from string, msg *pb.Message) error
}

// Transport interface for network communication
type Transport interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Broadcast(ctx context.Context, msg *pb.Message, excludeID string) error
	Send(ctx context.Context, clientID string, msg *pb.Message) error
	SetHandler(handler MessageHandler)
	GetConnections() []ConnectionInfo
}

// MessageHandler processes incoming messages
type MessageHandler func(ctx context.Context, from string, msg *pb.Message) error

// ConnectionInfo contains information about a connection
type ConnectionInfo struct {
	ID         string
	RemoteAddr string
	ChainID    string
}

// HTTPHandler provides HTTP endpoints
type HTTPHandler interface {
	RegisterRoutes() http.Handler
}
