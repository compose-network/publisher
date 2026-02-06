package quic

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

// RawMessageHandler processes incoming raw message bytes.
type RawMessageHandler func(ctx context.Context, clientID string, data []byte) error

// OnConnectHandler is called when a new client connection is established.
type OnConnectHandler func(ctx context.Context, clientID string, conn *Connection)

// Server manages incoming QUIC connections from sidecars.
type Server interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error

	Broadcast(ctx context.Context, msg proto.Message, excludeID string) error
	BroadcastRaw(ctx context.Context, data []byte, excludeID string) error
	Send(ctx context.Context, clientID string, msg proto.Message) error
	SendRaw(ctx context.Context, clientID string, data []byte) error

	SetHandler(handler RawMessageHandler)
	SetOnConnect(handler OnConnectHandler)

	GetConnections() []ConnectionInfo
	GetConnection(clientID string) *Connection
	ConnectionCount() int
	WaitForConnections(ctx context.Context, count int, timeout time.Duration) error
}

type server struct {
	mu          sync.RWMutex
	log         zerolog.Logger
	cfg         ServerConfig
	listener    *quic.Listener
	connections map[string]*Connection
	handler     RawMessageHandler
	onConnect   OnConnectHandler
	running     bool
	stopCh      chan struct{}
	wg          sync.WaitGroup
}

// NewServer creates a new QUIC server.
func NewServer(cfg ServerConfig, log zerolog.Logger) Server {
	return &server{
		log:         log.With().Str("component", "quic.server").Logger(),
		cfg:         cfg,
		connections: make(map[string]*Connection),
		stopCh:      make(chan struct{}),
	}
}

func (s *server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server already running")
	}

	tlsConfig := s.cfg.TLSConfig
	if tlsConfig == nil {
		var err error
		tlsConfig, err = GenerateSelfSignedCert()
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("generate TLS config: %w", err)
		}
		s.log.Warn().Msg("Using self-signed certificate for QUIC server")
	}

	quicConfig := &quic.Config{
		MaxIncomingStreams:      s.cfg.MaxIncomingStreams,
		MaxIncomingUniStreams:   s.cfg.MaxIncomingUniStreams,
		MaxIdleTimeout:          s.cfg.IdleTimeout,
		HandshakeIdleTimeout:    s.cfg.HandshakeIdleTimeout,
		KeepAlivePeriod:         serverKeepAlive(s.cfg),
		DisablePathMTUDiscovery: s.cfg.DisablePathMTUDiscovery,
		EnableDatagrams:         s.cfg.EnableDatagrams,
	}

	listener, err := quic.ListenAddr(s.cfg.ListenAddr, tlsConfig, quicConfig)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("listen: %w", err)
	}

	s.listener = listener
	s.running = true
	s.stopCh = make(chan struct{})
	s.mu.Unlock()

	s.log.Info().Str("addr", s.cfg.ListenAddr).Msg("QUIC server started")

	s.wg.Add(1)
	go s.acceptLoop(ctx)

	return nil
}

func (s *server) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}

	s.running = false
	close(s.stopCh)

	if s.listener != nil {
		s.listener.Close()
	}

	for _, conn := range s.connections {
		conn.Close()
	}
	s.connections = make(map[string]*Connection)
	s.mu.Unlock()

	s.wg.Wait()
	s.log.Info().Msg("QUIC server stopped")
	return nil
}

// --- Messaging ---

func (s *server) Broadcast(ctx context.Context, msg proto.Message, excludeID string) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	return s.BroadcastRaw(ctx, data, excludeID)
}

func (s *server) BroadcastRaw(ctx context.Context, data []byte, excludeID string) error {
	s.mu.RLock()
	connections := make([]*Connection, 0, len(s.connections))
	for id, conn := range s.connections {
		if id != excludeID {
			connections = append(connections, conn)
		}
	}
	s.mu.RUnlock()

	var lastErr error
	for _, conn := range connections {
		if err := conn.SendRaw(ctx, data); err != nil {
			s.log.Error().Err(err).Str("client_id", conn.ID()).Msg("Failed to send broadcast")
			lastErr = err
		}
	}

	return lastErr
}

func (s *server) Send(ctx context.Context, clientID string, msg proto.Message) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	return s.SendRaw(ctx, clientID, data)
}

func (s *server) SendRaw(ctx context.Context, clientID string, data []byte) error {
	s.mu.RLock()
	conn, ok := s.connections[clientID]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("client not found: %s", clientID)
	}

	return conn.SendRaw(ctx, data)
}

// --- Configuration ---

func (s *server) SetHandler(handler RawMessageHandler) {
	s.mu.Lock()
	s.handler = handler
	s.mu.Unlock()
}

func (s *server) SetOnConnect(handler OnConnectHandler) {
	s.mu.Lock()
	s.onConnect = handler
	s.mu.Unlock()
}

// --- Connection queries ---

func (s *server) GetConnections() []ConnectionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	infos := make([]ConnectionInfo, 0, len(s.connections))
	for _, conn := range s.connections {
		infos = append(infos, conn.Info())
	}
	return infos
}

func (s *server) GetConnection(clientID string) *Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connections[clientID]
}

func (s *server) ConnectionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.connections)
}

func (s *server) WaitForConnections(ctx context.Context, count int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if s.ConnectionCount() >= count {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for %d connections", count)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func serverKeepAlive(cfg ServerConfig) time.Duration {
	if cfg.DisableKeepAlive {
		return 0
	}
	if cfg.KeepAlivePeriod == 0 {
		return defaultKeepAlivePeriod
	}
	return cfg.KeepAlivePeriod
}
