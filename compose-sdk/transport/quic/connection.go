package quic

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"google.golang.org/protobuf/proto"
)

// MessageHandler processes incoming protobuf messages.
type MessageHandler func(ctx context.Context, clientID string, msg proto.Message) error

// Connection wraps a QUIC connection with message framing.
type Connection struct {
	mu             sync.RWMutex
	conn           quic.Connection
	id             string
	maxMessageSize uint32
	connectedAt    time.Time
	lastActivity   time.Time
}

// NewConnection creates a new Connection wrapper.
func NewConnection(conn quic.Connection, id string, maxMessageSize uint32) *Connection {
	now := time.Now()
	return &Connection{
		conn:           conn,
		id:             id,
		maxMessageSize: maxMessageSize,
		connectedAt:    now,
		lastActivity:   now,
	}
}

// ID returns the connection identifier.
func (c *Connection) ID() string {
	return c.id
}

// Context returns the connection context, which is closed when the connection closes.
func (c *Connection) Context() context.Context {
	return c.conn.Context()
}

// RemoteAddr returns the remote address of the connection.
func (c *Connection) RemoteAddr() string {
	return c.conn.RemoteAddr().String()
}

// Info returns connection information.
func (c *Connection) Info() ConnectionInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ConnectionInfo{
		ID:           c.id,
		RemoteAddr:   c.conn.RemoteAddr().String(),
		ConnectedAt:  c.connectedAt,
		LastActivity: c.lastActivity,
	}
}

// Close closes the connection.
func (c *Connection) Close() error {
	return c.conn.CloseWithError(0, "closing connection")
}

// OpenStream opens a new bidirectional stream.
func (c *Connection) OpenStream(ctx context.Context) (quic.Stream, error) {
	return c.conn.OpenStreamSync(ctx)
}

// AcceptStream accepts an incoming stream.
func (c *Connection) AcceptStream(ctx context.Context) (quic.Stream, error) {
	return c.conn.AcceptStream(ctx)
}

// SendMessage sends a protobuf message over a new stream.
func (c *Connection) SendMessage(ctx context.Context, msg proto.Message) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	return c.SendRaw(ctx, data)
}

// SendRaw sends raw bytes over a new stream.
func (c *Connection) SendRaw(ctx context.Context, data []byte) error {
	if uint32(len(data)) > c.maxMessageSize {
		return fmt.Errorf("message size %d exceeds maximum %d", len(data), c.maxMessageSize)
	}

	stream, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	defer stream.Close()

	if err := writeFrame(stream, data); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}

	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()

	return nil
}

// ReadMessage reads a protobuf message from a stream.
func (c *Connection) ReadMessage(stream quic.Stream, msg proto.Message) error {
	data, err := readFrame(stream, c.maxMessageSize)
	if err != nil {
		return fmt.Errorf("read frame: %w", err)
	}

	if err := proto.Unmarshal(data, msg); err != nil {
		return fmt.Errorf("unmarshal message: %w", err)
	}

	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()

	return nil
}

// RecordActivity updates the last-activity timestamp.
func (c *Connection) RecordActivity() {
	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()
}

// writeFrame writes a length-prefixed frame to the writer.
func writeFrame(w io.Writer, data []byte) error {
	// Write 4-byte length prefix (big endian)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))

	if _, err := w.Write(lenBuf); err != nil {
		return fmt.Errorf("write length: %w", err)
	}

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write data: %w", err)
	}

	return nil
}

// readFrame reads a length-prefixed frame from the reader.
func readFrame(r io.Reader, maxSize uint32) ([]byte, error) {
	// Read 4-byte length prefix (big endian)
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return nil, fmt.Errorf("read length: %w", err)
	}

	msgLen := binary.BigEndian.Uint32(lenBuf)
	if msgLen > maxSize {
		return nil, fmt.Errorf("message size %d exceeds maximum %d", msgLen, maxSize)
	}

	data := make([]byte, msgLen)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("read data: %w", err)
	}

	return data, nil
}
