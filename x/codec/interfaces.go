package codec

import (
	"io"

	"google.golang.org/protobuf/proto"
)

// Codec defines the message encoding/decoding interface
type Codec interface {
	Encode(msg proto.Message) ([]byte, error)
	Decode(data []byte, msg proto.Message) error
	MaxMessageSize() int
}

// StreamCodec extends Codec for streaming operations
type StreamCodec interface {
	Codec
	DecodeStream(r io.Reader, msg proto.Message) error
	EncodeStream(w io.Writer, msg proto.Message) error
}

// Registry manages multiple codec implementations
type Registry interface {
	Register(name string, codec Codec)
	Get(name string) (Codec, bool)
	Default() Codec
}
