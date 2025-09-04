package codec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type dummyCodec struct{ size int }

func (d *dummyCodec) Encode(_ proto.Message) ([]byte, error) { return []byte{1, 2, 3}, nil }
func (d *dummyCodec) Decode(_ []byte, _ proto.Message) error { return nil }
func (d *dummyCodec) MaxMessageSize() int                    { return d.size }

func TestRegistry_Default(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	def := r.Default()
	require.NotNil(t, def)

	p, ok := def.(*ProtobufCodec)
	require.True(t, ok)
	assert.Equal(t, 10*1024*1024, p.MaxMessageSize())
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.Register("custom", &dummyCodec{size: 42})

	got, ok := r.Get("custom")
	require.True(t, ok)
	assert.Equal(t, 42, got.MaxMessageSize())
}
