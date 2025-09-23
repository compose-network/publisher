package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/ssvlabs/rollup-shared-publisher/proto/rollup/v1"
)

func TestMessageType_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		msgType  MessageType
		expected string
	}{
		{
			name:     "StartSlot message type",
			msgType:  MsgStartSlot,
			expected: "StartSlot",
		},
		{
			name:     "RequestSeal message type",
			msgType:  MsgRequestSeal,
			expected: "RequestSeal",
		},
		{
			name:     "L2Block message type",
			msgType:  MsgL2Block,
			expected: "L2Block",
		},
		{
			name:     "StartSC message type",
			msgType:  MsgStartSC,
			expected: "StartSC",
		},
		{
			name:     "RollBackAndStartSlot message type",
			msgType:  MsgRollBackAndStartSlot,
			expected: "RollBackAndStartSlot",
		},
		{
			name:     "unknown message type",
			msgType:  MessageType(99),
			expected: "Unknown(99)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := tt.msgType.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMessageType_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		msgType MessageType
		valid   bool
	}{
		{
			name:    "StartSlot is valid",
			msgType: MsgStartSlot,
			valid:   true,
		},
		{
			name:    "RequestSeal is valid",
			msgType: MsgRequestSeal,
			valid:   true,
		},
		{
			name:    "L2Block is valid",
			msgType: MsgL2Block,
			valid:   true,
		},
		{
			name:    "StartSC is valid",
			msgType: MsgStartSC,
			valid:   true,
		},
		{
			name:    "RollBackAndStartSlot is valid",
			msgType: MsgRollBackAndStartSlot,
			valid:   true,
		},
		{
			name:    "zero value is invalid",
			msgType: MessageType(0),
			valid:   false,
		},
		{
			name:    "unknown message type is invalid",
			msgType: MessageType(99),
			valid:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := tt.msgType.IsValid()
			assert.Equal(t, tt.valid, result)
		})
	}
}

func TestClassifyMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		msg      *pb.Message
		expected MessageType
		valid    bool
	}{
		{
			name: "StartSlot message is correctly classified",
			msg: &pb.Message{
				Payload: &pb.Message_StartSlot{
					StartSlot: &pb.StartSlot{
						Slot:                 1,
						NextSuperblockNumber: 1,
						LastSuperblockHash:   []byte("hash"),
						L2BlocksRequest: []*pb.L2BlockRequest{
							{
								ChainId:     []byte("chain1"),
								BlockNumber: 1,
								ParentHash:  []byte("parent"),
							},
						},
					},
				},
			},
			expected: MsgStartSlot,
			valid:    true,
		},
		{
			name: "RequestSeal message is correctly classified",
			msg: &pb.Message{
				Payload: &pb.Message_RequestSeal{
					RequestSeal: &pb.RequestSeal{
						Slot:        1,
						IncludedXts: [][]byte{[]byte("xt1"), []byte("xt2")},
					},
				},
			},
			expected: MsgRequestSeal,
			valid:    true,
		},
		{
			name: "L2Block message is correctly classified",
			msg: &pb.Message{
				Payload: &pb.Message_L2Block{
					L2Block: &pb.L2Block{
						Slot:            1,
						ChainId:         []byte("chain1"),
						BlockNumber:     1,
						BlockHash:       []byte("hash"),
						ParentBlockHash: []byte("parent"),
						IncludedXts:     [][]byte{[]byte("xt1")},
						Block:           []byte("blockdata"),
					},
				},
			},
			expected: MsgL2Block,
			valid:    true,
		},
		{
			name: "StartSC message is correctly classified",
			msg: &pb.Message{
				Payload: &pb.Message_StartSc{
					StartSc: &pb.StartSC{
						Slot:             1,
						XtSequenceNumber: 1,
						XtId:             []byte("xtid"),
						XtRequest: &pb.XTRequest{
							Transactions: []*pb.TransactionRequest{
								{
									ChainId:     []byte("chain1"),
									Transaction: [][]byte{[]byte("tx1")},
								},
							},
						},
					},
				},
			},
			expected: MsgStartSC,
			valid:    true,
		},
		{
			name: "RollBackAndStartSlot message is correctly classified",
			msg: &pb.Message{
				Payload: &pb.Message_RollBackAndStartSlot{
					RollBackAndStartSlot: &pb.RollBackAndStartSlot{
						CurrentSlot:          1,
						NextSuperblockNumber: 1,
						LastSuperblockHash:   []byte("hash"),
						L2BlocksRequest: []*pb.L2BlockRequest{
							{
								ChainId:     []byte("chain1"),
								BlockNumber: 1,
								ParentHash:  []byte("parent"),
							},
						},
					},
				},
			},
			expected: MsgRollBackAndStartSlot,
			valid:    true,
		},
		{
			name: "non-SBCP message (Vote) returns invalid",
			msg: &pb.Message{
				Payload: &pb.Message_Vote{
					Vote: &pb.Vote{
						SenderChainId: []byte("chain1"),
						XtId:          &pb.XtID{Hash: []byte("xtid")},
						Vote:          true,
					},
				},
			},
			expected: MessageType(0),
			valid:    false,
		},
		{
			name: "non-SBCP message (XTRequest) returns invalid",
			msg: &pb.Message{
				Payload: &pb.Message_XtRequest{
					XtRequest: &pb.XTRequest{
						Transactions: []*pb.TransactionRequest{
							{
								ChainId:     []byte("chain1"),
								Transaction: [][]byte{[]byte("tx1")},
							},
						},
					},
				},
			},
			expected: MessageType(0),
			valid:    false,
		},
		{
			name:     "nil message returns invalid",
			msg:      nil,
			expected: MessageType(0),
			valid:    false,
		},
		{
			name: "message with nil payload returns invalid",
			msg: &pb.Message{
				Payload: nil,
			},
			expected: MessageType(0),
			valid:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Act
			msgType, valid := ClassifyMessage(tt.msg)

			// Assert
			assert.Equal(t, tt.expected, msgType, "message type should match expected")
			assert.Equal(t, tt.valid, valid, "validity should match expected")
		})
	}
}

func TestIsSBCPMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		msg    *pb.Message
		isSBCP bool
	}{
		{
			name: "StartSlot is SBCP message",
			msg: &pb.Message{
				Payload: &pb.Message_StartSlot{
					StartSlot: &pb.StartSlot{
						Slot:                 1,
						NextSuperblockNumber: 1,
						L2BlocksRequest:      []*pb.L2BlockRequest{},
					},
				},
			},
			isSBCP: true,
		},
		{
			name: "RequestSeal is SBCP message",
			msg: &pb.Message{
				Payload: &pb.Message_RequestSeal{
					RequestSeal: &pb.RequestSeal{
						Slot: 1,
					},
				},
			},
			isSBCP: true,
		},
		{
			name: "L2Block is SBCP message",
			msg: &pb.Message{
				Payload: &pb.Message_L2Block{
					L2Block: &pb.L2Block{
						Slot:    1,
						ChainId: []byte("chain1"),
					},
				},
			},
			isSBCP: true,
		},
		{
			name: "StartSC is SBCP message",
			msg: &pb.Message{
				Payload: &pb.Message_StartSc{
					StartSc: &pb.StartSC{
						Slot: 1,
						XtId: []byte("xtid"),
					},
				},
			},
			isSBCP: true,
		},
		{
			name: "RollBackAndStartSlot is SBCP message",
			msg: &pb.Message{
				Payload: &pb.Message_RollBackAndStartSlot{
					RollBackAndStartSlot: &pb.RollBackAndStartSlot{
						CurrentSlot: 1,
					},
				},
			},
			isSBCP: true,
		},
		{
			name: "Vote is not SBCP message",
			msg: &pb.Message{
				Payload: &pb.Message_Vote{
					Vote: &pb.Vote{},
				},
			},
			isSBCP: false,
		},
		{
			name: "Decided is not SBCP message",
			msg: &pb.Message{
				Payload: &pb.Message_Decided{
					Decided: &pb.Decided{},
				},
			},
			isSBCP: false,
		},
		{
			name: "XTRequest is not SBCP message",
			msg: &pb.Message{
				Payload: &pb.Message_XtRequest{
					XtRequest: &pb.XTRequest{},
				},
			},
			isSBCP: false,
		},
		{
			name: "CIRCMessage is not SBCP message",
			msg: &pb.Message{
				Payload: &pb.Message_CircMessage{
					CircMessage: &pb.CIRCMessage{},
				},
			},
			isSBCP: false,
		},
		{
			name:   "nil message is not SBCP",
			msg:    nil,
			isSBCP: false,
		},
		{
			name: "empty message is not SBCP",
			msg: &pb.Message{
				Payload: nil,
			},
			isSBCP: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := IsSBCPMessage(tt.msg)
			assert.Equal(t, tt.isSBCP, result)
		})
	}
}

func TestMessageType_Completeness(t *testing.T) {
	t.Parallel()

	t.Run("all message types have valid string representation", func(t *testing.T) {
		t.Parallel()

		validTypes := []MessageType{
			MsgStartSlot,
			MsgRequestSeal,
			MsgL2Block,
			MsgStartSC,
			MsgRollBackAndStartSlot,
		}

		for _, msgType := range validTypes {
			str := msgType.String()
			require.NotEmpty(t, str, "message type %d should have non-empty string representation", msgType)
			require.NotContains(t, str, "Unknown", "valid message type should not contain 'Unknown'")
		}
	})

	t.Run("all valid message types are recognized as valid", func(t *testing.T) {
		t.Parallel()

		validTypes := []MessageType{
			MsgStartSlot,
			MsgRequestSeal,
			MsgL2Block,
			MsgStartSC,
			MsgRollBackAndStartSlot,
		}

		for _, msgType := range validTypes {
			assert.True(t, msgType.IsValid(), "message type %s should be valid", msgType.String())
		}
	})
}
