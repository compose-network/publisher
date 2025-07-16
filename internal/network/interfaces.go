package network

import (
	"context"
	"net"
	"time"

	pb "github.com/ssvlabs/rollup-shared-publisher/internal/proto"
)

// Server interface defines the server contract
type Server interface {
	// Start starts the server
	Start(ctx context.Context) error
	// Stop gracefully stops the server
	Stop(ctx context.Context) error
	// Broadcast sends a message to all connected clients except the excluded one
	Broadcast(ctx context.Context, msg *pb.Message, excludeID string) error
	// Send sends a message to a specific client
	Send(ctx context.Context, clientID string, msg *pb.Message) error
	// SetHandler sets the message handler
	SetHandler(handler MessageHandler)
	// GetConnections returns all active connections
	GetConnections() []ConnectionInfo
}

// Client interface defines the client contract
type Client interface {
	// Connect establishes connection to the server
	Connect(ctx context.Context) error
	// Disconnect closes the connection
	Disconnect(ctx context.Context) error
	// Send sends a message to the server
	Send(ctx context.Context, msg *pb.Message) error
	// SetHandler sets the message handler for received messages
	SetHandler(handler MessageHandler)
	// IsConnected returns connection status
	IsConnected() bool
	// GetID returns the client identifier
	GetID() string
}

// MessageHandler processes incoming messages
type MessageHandler func(ctx context.Context, from string, msg *pb.Message) error

// ConnectionInfo contains information about a connection
type ConnectionInfo struct {
	ID          string
	RemoteAddr  string
	ConnectedAt time.Time
	LastSeen    time.Time
	ChainID     string
}

// Connection represents a network connection
type Connection interface {
	net.Conn
	GetID() string
	GetInfo() ConnectionInfo
	UpdateLastSeen()
}
