package codec

import (
	"sync"
)

// registry implements Registry interface
type registry struct {
	mu       sync.RWMutex
	codecs   map[string]Codec
	default_ string
}

// NewRegistry creates a new codec registry
func NewRegistry() Registry {
	r := &registry{
		codecs: make(map[string]Codec),
	}

	// Register default protobuf codec
	defaultCodec := NewProtobufCodec(10 * 1024 * 1024) // 10MB default
	r.Register("protobuf", defaultCodec)
	r.default_ = "protobuf"

	return r
}

// Register registers a codec with a name
func (r *registry) Register(name string, codec Codec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.codecs[name] = codec
}

// Get retrieves a codec by name
func (r *registry) Get(name string) (Codec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	codec, exists := r.codecs[name]
	return codec, exists
}

// Default returns the default codec
func (r *registry) Default() Codec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.codecs[r.default_]
}
