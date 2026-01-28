// Package quic provides QUIC-based transport for publisher-sidecar communication.
package quic

import (
	"crypto/tls"
	"time"
)

const (
	defaultKeepAlivePeriod = 15 * time.Second
	defaultDialTimeout     = 3 * time.Second
)

// ServerConfig holds configuration for a QUIC server.
type ServerConfig struct {
	// ListenAddr is the address to listen on (e.g., ":8080").
	ListenAddr string

	// TLSConfig is the TLS configuration for the server.
	// If nil, a self-signed certificate will be generated.
	TLSConfig *tls.Config

	// MaxIncomingStreams is the maximum number of concurrent streams per connection.
	MaxIncomingStreams int64

	// MaxIncomingUniStreams is the maximum number of concurrent unidirectional streams per connection.
	MaxIncomingUniStreams int64

	// IdleTimeout is the maximum time a connection can be idle before being closed.
	IdleTimeout time.Duration

	// HandshakeIdleTimeout is the idle timeout before completion of the handshake.
	HandshakeIdleTimeout time.Duration

	// KeepAlivePeriod controls how often PING frames are sent to keep the connection alive.
	KeepAlivePeriod time.Duration

	// DisableKeepAlive disables automatic keep-alive PINGs when KeepAlivePeriod is zero.
	DisableKeepAlive bool

	// DisablePathMTUDiscovery disables QUIC path MTU discovery.
	DisablePathMTUDiscovery bool

	// EnableDatagrams enables QUIC datagram support (RFC 9221).
	EnableDatagrams bool

	// MaxMessageSize is the maximum size of a single message in bytes.
	MaxMessageSize uint32
}

// DefaultServerConfig returns a ServerConfig with default values.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		ListenAddr:            ":8080",
		MaxIncomingStreams:    100,
		MaxIncomingUniStreams: 100,
		HandshakeIdleTimeout:  5 * time.Second,
		IdleTimeout:           60 * time.Second,
		KeepAlivePeriod:       defaultKeepAlivePeriod,
		MaxMessageSize:        10 * 1024 * 1024, // 10MB
	}
}

// ClientConfig holds configuration for a QUIC client.
type ClientConfig struct {
	// ServerAddr is the address of the QUIC server to connect to.
	ServerAddr string

	// TLSConfig is the TLS configuration for the client.
	// If nil, InsecureSkipVerify will be used (for development).
	TLSConfig *tls.Config

	// ClientID is an identifier for this client (e.g., chain ID).
	ClientID string

	// ReconnectDelay is the delay between reconnection attempts.
	ReconnectDelay time.Duration

	// MaxRetries is the maximum number of connection attempts.
	MaxRetries int

	// DisableAutoReconnect disables automatic reconnection attempts when not connected.
	DisableAutoReconnect bool

	// IdleTimeout is the maximum time a connection can be idle.
	IdleTimeout time.Duration

	// HandshakeIdleTimeout is the idle timeout before completion of the handshake.
	HandshakeIdleTimeout time.Duration

	// DialTimeout is the max time allowed for the QUIC handshake.
	DialTimeout time.Duration

	// KeepAlivePeriod controls how often PING frames are sent to keep the connection alive.
	KeepAlivePeriod time.Duration

	// DisableKeepAlive disables automatic keep-alive PINGs when KeepAlivePeriod is zero.
	DisableKeepAlive bool

	// MaxIncomingStreams is the maximum number of concurrent bidirectional streams allowed from the peer.
	MaxIncomingStreams int64

	// MaxIncomingUniStreams is the maximum number of concurrent unidirectional streams allowed from the peer.
	MaxIncomingUniStreams int64

	// DisablePathMTUDiscovery disables QUIC path MTU discovery.
	DisablePathMTUDiscovery bool

	// EnableDatagrams enables QUIC datagram support (RFC 9221).
	EnableDatagrams bool

	// MaxMessageSize is the maximum size of a single message in bytes.
	MaxMessageSize uint32
}

// DefaultClientConfig returns a ClientConfig with default values.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		ServerAddr:            "localhost:8080",
		ReconnectDelay:        5 * time.Second,
		MaxRetries:            10,
		HandshakeIdleTimeout:  5 * time.Second,
		DialTimeout:           defaultDialTimeout,
		IdleTimeout:           60 * time.Second,
		KeepAlivePeriod:       defaultKeepAlivePeriod,
		MaxIncomingStreams:    100,
		MaxIncomingUniStreams: 100,
		MaxMessageSize:        10 * 1024 * 1024, // 10MB
	}
}

// ConnectionInfo holds information about an active connection.
type ConnectionInfo struct {
	// ID is the unique identifier for this connection.
	ID string

	// RemoteAddr is the remote address of the connection.
	RemoteAddr string

	// ConnectedAt is when the connection was established.
	ConnectedAt time.Time

	// LastActivity is the time of the last message sent or received.
	LastActivity time.Time
}
