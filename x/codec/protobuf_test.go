package codec

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/compose-network/specs/compose/proto"
	"google.golang.org/protobuf/proto"
)

func TestProtobufCodec_EncodeDecode_Roundtrip(t *testing.T) {
	t.Parallel()

	c := NewProtobufCodec(1 << 20) // 1MB

	msgIn := &pb.Message{
		SenderId: "test-sender",
		Payload: &pb.Message_Vote{Vote: &pb.Vote{
			InstanceId: bytes.Repeat([]byte{'h'}, 32),
			ChainId:    10,
			Vote:       true,
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
		Payload: &pb.Message_Decided{
			Decided: &pb.Decided{
				InstanceId: []byte{'a'},
				Decision:   true,
			},
		},
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
		Payload: &pb.Message_XtRequest{
			XtRequest: &pb.XTRequest{
				TransactionRequests: []*pb.TransactionRequest{
					{
						ChainId:     10,
						Transaction: [][]byte{bytes.Repeat([]byte{0xcd}, 100)},
					},
				},
			},
		},
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
