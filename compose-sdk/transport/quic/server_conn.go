package quic

import (
	"context"

	"github.com/quic-go/quic-go"
)

// acceptLoop accepts new QUIC connections until the server is stopped.
func (s *server) acceptLoop(ctx context.Context) {
	defer s.wg.Done()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}

		conn, err := s.listener.Accept(ctx)
		if err != nil {
			select {
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			default:
				s.log.Error().Err(err).Msg("Accept failed")
				continue
			}
		}

		s.wg.Add(1)
		go s.handleConnection(ctx, conn)
	}
}

// handleConnection performs the client identification handshake and then
// enters the stream-reading loop for the lifetime of the connection.
func (s *server) handleConnection(ctx context.Context, qconn quic.Connection) {
	defer s.wg.Done()

	stream, err := qconn.AcceptStream(ctx)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to accept identification stream")
		qconn.CloseWithError(1, "identification failed")
		return
	}

	idBuf, err := readFrame(stream, 256)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to read client ID")
		stream.Close()
		qconn.CloseWithError(1, "identification failed")
		return
	}
	stream.Close()

	clientID := string(idBuf)
	conn := NewConnection(qconn, clientID, s.cfg.MaxMessageSize)

	s.mu.Lock()
	if existing, ok := s.connections[clientID]; ok {
		s.log.Warn().Str("client_id", clientID).Msg("Client reconnected, closing old connection")
		existing.Close()
	}
	s.connections[clientID] = conn
	onConnect := s.onConnect
	s.mu.Unlock()

	s.log.Info().
		Str("client_id", clientID).
		Str("remote_addr", conn.RemoteAddr()).
		Msg("Client connected")

	if onConnect != nil {
		onConnect(ctx, clientID, conn)
	}

	defer func() {
		s.mu.Lock()
		delete(s.connections, clientID)
		s.mu.Unlock()
		conn.Close()
		s.log.Info().Str("client_id", clientID).Msg("Client disconnected")
	}()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}

		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			select {
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			default:
				s.log.Debug().Err(err).Str("client_id", clientID).Msg("Stream accept failed")
				return
			}
		}

		s.wg.Add(1)
		go s.handleStream(ctx, conn, stream)
	}
}

// handleStream reads a single message from a stream and dispatches it
// to the registered handler.
func (s *server) handleStream(ctx context.Context, conn *Connection, stream quic.Stream) {
	defer s.wg.Done()
	defer stream.Close()

	data, err := readFrame(stream, s.cfg.MaxMessageSize)
	if err != nil {
		s.log.Debug().Err(err).Str("client_id", conn.ID()).Msg("Failed to read message")
		return
	}
	conn.RecordActivity()

	s.mu.RLock()
	handler := s.handler
	s.mu.RUnlock()

	if handler != nil {
		if err := handler(ctx, conn.ID(), data); err != nil {
			s.log.Error().Err(err).Str("client_id", conn.ID()).Msg("Handler failed")
		}
	}
}
