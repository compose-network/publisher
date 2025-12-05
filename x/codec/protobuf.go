package codec

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sync"

	"google.golang.org/protobuf/proto"
)

// ProtobufCodec implements length-prefixed protobuf encoding
type ProtobufCodec struct {
	maxMessageSize int

	// Buffer pools for zero-allocation operations
	bufferPool  sync.Pool
	scratchPool sync.Pool
}

// NewProtobufCodec creates a new protobuf codec
func NewProtobufCodec(maxMessageSize int) *ProtobufCodec {
	return &ProtobufCodec{
		maxMessageSize: maxMessageSize,
		bufferPool: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 0, 1024) // Start with 1KB capacity
				return &buf
			},
		},
		scratchPool: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 4096) // 4KB scratch buffer
				return &buf
			},
		},
	}
}

// Encode marshals a message with length prefix
func (c *ProtobufCodec) Encode(msg proto.Message) ([]byte, error) {
	// Get reusable buffer from pool
	bufPtr := c.bufferPool.Get().(*[]byte)
	defer c.bufferPool.Put(bufPtr)

	// Reset buffer but keep capacity
	buf := (*bufPtr)[:0]

	// Marshal protobuf to pooled buffer first
	data, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal: %w", err)
	}

	dataLen := len(data)
	if dataLen > c.maxMessageSize {
		return nil, fmt.Errorf("message size %d exceeds max %d", dataLen, c.maxMessageSize)
	}

	// Ensure buffer has enough capacity
	totalSize := 4 + dataLen
	if cap(buf) < totalSize {
		buf = make([]byte, 0, totalSize)
	}

	// Build message in pooled buffer
	buf = buf[:totalSize]

	// Write length prefix (big endian) - safe conversion after bounds check
	if dataLen > math.MaxUint32 {
		return nil, fmt.Errorf("message size %d exceeds uint32 max", dataLen)
	}
	binary.BigEndian.PutUint32(buf[:4], uint32(dataLen))

	// Copy message data
	copy(buf[4:], data)

	// Return copy since buf goes back to pool
	result := make([]byte, totalSize)
	copy(result, buf)

	return result, nil
}

// Decode unmarshals a length-prefixed message
func (c *ProtobufCodec) Decode(data []byte, msg proto.Message) error {
	if len(data) < 4 {
		return fmt.Errorf("data too short for length prefix")
	}

	length := binary.BigEndian.Uint32(data[:4])
	if int(length) > c.maxMessageSize {
		return fmt.Errorf("message size %d exceeds max %d", length, c.maxMessageSize)
	}

	if len(data) < int(4+length) {
		return fmt.Errorf("data too short for claimed message length")
	}

	messageData := data[4 : 4+length]
	return proto.Unmarshal(messageData, msg)
}

// DecodeStream reads a length-prefixed message from stream
func (c *ProtobufCodec) DecodeStream(r io.Reader, msg proto.Message) error {
	// Read length prefix
	lengthBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lengthBuf); err != nil {
		return err
	}

	length := binary.BigEndian.Uint32(lengthBuf)
	if int(length) > c.maxMessageSize {
		return fmt.Errorf("message size %d exceeds max %d", length, c.maxMessageSize)
	}

	if length == 0 {
		return fmt.Errorf("empty message")
	}

	// Get scratch buffer from pool
	scratchPtr := c.scratchPool.Get().(*[]byte)
	defer c.scratchPool.Put(scratchPtr)

	scratch := *scratchPtr
	var messageData []byte

	// Use scratch buffer if message fits, otherwise allocate
	if int(length) <= len(scratch) {
		messageData = scratch[:length]
	} else {
		messageData = make([]byte, length)
	}

	// Read message data
	if _, err := io.ReadFull(r, messageData); err != nil {
		return err
	}

	// Unmarshal protobuf
	return proto.Unmarshal(messageData, msg)
}

// EncodeStream writes a length-prefixed message to stream
func (c *ProtobufCodec) EncodeStream(w io.Writer, msg proto.Message) error {
	data, err := c.Encode(msg)
	if err != nil {
		return err
	}

	_, err = w.Write(data)
	return err
}

// MaxMessageSize returns the maximum message size
func (c *ProtobufCodec) MaxMessageSize() int {
	return c.maxMessageSize
}
