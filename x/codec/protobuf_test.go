package codec

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/compose-network/publisher/proto/rollup/v1"
	"google.golang.org/protobuf/proto"
)

func TestProtobufCodec_EncodeDecode_Roundtrip(t *testing.T) {
	t.Parallel()

	c := NewProtobufCodec(1 << 20) // 1MB

	msgIn := &pb.Message{
		SenderId: "test-sender",
		Payload: &pb.Message_Vote{Vote: &pb.Vote{
			SenderChainId: []byte("chain-A"),
			XtId:          &pb.XtID{Hash: bytes.Repeat([]byte{'h'}, 32)},
			Vote:          true,
		}},
	}

	data, err := c.Encode(msgIn)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	var msgOut pb.Message
	require.NoError(t, c.Decode(data, &msgOut))
	assert.True(t, proto.Equal(msgIn, &msgOut))
}

func TestProtobufCodec_EncodeStream_DecodeStream(t *testing.T) {
	t.Parallel()

	c := NewProtobufCodec(1 << 20)
	buf := new(bytes.Buffer)

	msgIn := &pb.Message{
		SenderId: "streamer",
		Payload: &pb.Message_Block{Block: &pb.Block{
			ChainId:   []byte("X"),
			BlockData: bytes.Repeat([]byte{0xab}, 256),
		}},
	}

	require.NoError(t, c.EncodeStream(buf, msgIn))

	var msgOut pb.Message
	require.NoError(t, c.DecodeStream(buf, &msgOut))
	assert.True(t, proto.Equal(msgIn, &msgOut))
}

func TestProtobufCodec_MaxSizeExceeded_OnEncode(t *testing.T) {
	t.Parallel()

	c := NewProtobufCodec(16)

	msg := &pb.Message{
		Payload: &pb.Message_Block{Block: &pb.Block{
			ChainId:   []byte("Y"),
			BlockData: bytes.Repeat([]byte{0xcd}, 100),
		}},
	}

	_, err := c.Encode(msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds max")
}

func TestProtobufCodec_Decode_TruncatedPayload(t *testing.T) {
	t.Parallel()

	c := NewProtobufCodec(1024)

	data := make([]byte, 4+6)
	binary.BigEndian.PutUint32(data[:4], 10)
	copy(data[4:], "123456")

	var msg pb.Message
	err := c.Decode(data, &msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "data too short")
}

func TestProtobufCodec_DecodeStream_Empty(t *testing.T) {
	t.Parallel()

	c := NewProtobufCodec(1024)
	buf := bytes.NewBuffer([]byte{0, 0, 0, 0})

	var msg pb.Message
	err := c.DecodeStream(buf, &msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty message")
}
