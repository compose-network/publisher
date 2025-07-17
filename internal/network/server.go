package network

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	pb "github.com/ssvlabs/rollup-shared-publisher/internal/proto"
)

// ServerConfig contains server configuration.
type ServerConfig struct {
	ListenAddr     string
	WriteTimeout   time.Duration
	MaxMessageSize int
	MaxConnections int
}

// server implements the Server interface
type server struct {
	cfg      ServerConfig
	listener net.Listener
	handler  MessageHandler
	codec    *Codec
	log      zerolog.Logger

	connections sync.Map // map[string]Connection
	writers     sync.Map // map[string]*StreamWriter

	running atomic.Bool
	wg      sync.WaitGroup
}

// NewServer creates a new server instance.
func NewServer(cfg ServerConfig, log zerolog.Logger) Server {
	return &server{
		cfg:   cfg,
		codec: NewCodec(cfg.MaxMessageSize),
		log:   log.With().Str("component", "server").Logger(),
	}
}

// Start starts the server.
func (s *server) Start(ctx context.Context) error {
	if !s.running.CompareAndSwap(false, true) {
		return ErrServerRunning
	}

	listener, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		s.running.Store(false)
		return fmt.Errorf("failed to listen: %w", err)
	}
	s.listener = listener

	s.log.Info().
		Str("addr", s.cfg.ListenAddr).
		Int("max_connections", s.cfg.MaxConnections).
		Msg("Server started")

	s.wg.Add(1)
	go s.acceptLoop(ctx)

	return nil
}

// Stop gracefully stops the server.
func (s *server) Stop(ctx context.Context) error {
	if !s.running.CompareAndSwap(true, false) {
		return ErrServerNotRunning
	}

	s.log.Info().Msg("Stopping server")

	// Close listener
	if err := s.listener.Close(); err != nil {
		s.log.Error().Err(err).Msg("Failed to close listener")
	}

	// Close all connections
	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := value.(Connection); ok {
			conn.Close()
		}
		return true
	})

	// Wait for all goroutines with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.log.Info().Msg("Server stopped gracefully")
	case <-ctx.Done():
		s.log.Warn().Msg("Server stop timeout")
		return ctx.Err()
	}

	return nil
}

// SetHandler sets the message handler.
func (s *server) SetHandler(handler MessageHandler) {
	s.handler = handler
}

// acceptLoop accepts new connections.
func (s *server) acceptLoop(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Accept with timeout
			s.listener.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second))

			netConn, err := s.listener.Accept()
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				if s.running.Load() {
					s.log.Error().Err(err).Msg("Accept error")
				}
				return
			}

			// Check connection limit
			connCount := 0
			s.connections.Range(func(_, _ interface{}) bool {
				connCount++
				return true
			})

			if s.cfg.MaxConnections > 0 && connCount >= s.cfg.MaxConnections {
				s.log.Warn().
					Int("current", connCount).
					Int("max", s.cfg.MaxConnections).
					Msg(ErrConnectionLimit.Error())
				netConn.Close()
				continue
			}

			s.wg.Add(1)
			go s.handleConnection(ctx, netConn)
		}
	}
}

// handleConnection manages lifecycle and communication for a single client connection.
// It sets up keepalive, handles message decoding, and invokes the registered handler.
// Ensures proper cleanup of resources on disconnection or errors.
func (s *server) handleConnection(ctx context.Context, netConn net.Conn) {
	defer s.wg.Done()

	if tcpConn, ok := netConn.(*net.TCPConn); ok {
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	// Generate connection ID
	connID := uuid.New().String()
	conn := NewConnection(netConn, connID)

	log := s.log.With().
		Str("conn_id", connID).
		Str("remote_addr", netConn.RemoteAddr().String()).
		Logger()

	// Store connection
	s.connections.Store(connID, conn)
	writer := NewStreamWriter(conn, s.codec)
	s.writers.Store(connID, writer)

	defer func() {
		conn.Close()
		s.connections.Delete(connID)
		s.writers.Delete(connID)
		writer.Close()
		log.Info().Msg("Connection closed")
	}()

	log.Info().Msg("New connection")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			var msg pb.Message
			if err := s.codec.Decode(conn, &msg); err != nil {
				if err == io.EOF {
					log.Debug().Msg("Client disconnected")
				} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
					log.Debug().Msg("Read timeout - continuing to keep connection alive")
					continue
				} else {
					log.Error().Err(err).Msg("Read error")
				}
				return
			}

			conn.UpdateLastSeen()

			if s.handler != nil {
				if err := s.handler(ctx, connID, &msg); err != nil {
					log.Error().Err(err).Msg("Handler error")
				}
			}
		}
	}
}

// Broadcast sends a message to all clients except excluded.
func (s *server) Broadcast(ctx context.Context, msg *pb.Message, excludeID string) error {
	var (
		wg      sync.WaitGroup
		errChan = make(chan error, 1)
		sent    atomic.Int32
	)

	s.writers.Range(func(key, value interface{}) bool {
		connID := key.(string)
		writer := value.(*StreamWriter)

		if connID == excludeID {
			return true
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := writer.Write(msg); err != nil {
				s.log.Error().
					Str("conn_id", connID).
					Err(err).
					Msg("Broadcast write error")
				select {
				case errChan <- err:
				default:
				}
			} else {
				sent.Add(1)
			}
		}()

		return true
	})

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.log.Debug().
			Int32("sent", sent.Load()).
			Msg("Broadcast complete")
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errChan:
		return err
	}

	return nil
}

// Send sends a message to a specific client.
func (s *server) Send(_ context.Context, clientID string, msg *pb.Message) error {
	writer, ok := s.writers.Load(clientID)
	if !ok {
		return fmt.Errorf("client %s not found", clientID)
	}

	return writer.(*StreamWriter).Write(msg)
}

// GetConnections returns all active connections.
func (s *server) GetConnections() []ConnectionInfo {
	var connections []ConnectionInfo

	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := value.(Connection); ok {
			connections = append(connections, conn.GetInfo())
		}
		return true
	})

	return connections
}
