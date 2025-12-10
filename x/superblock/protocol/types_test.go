package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/compose-network/specs/compose/proto"
)

func TestMessageType_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		msgType  MessageType
		expected string
	}{
		{
			name:     "StartPeriod message type",
			msgType:  MsgStartPeriod,
			expected: "StartPeriod",
		},
		{
			name:     "StartInstance message type",
			msgType:  MsgStartInstance,
			expected: "StartInstance",
		},
		{
			name:     "Rollback message type",
			msgType:  MsgRollback,
			expected: "Rollback",
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
			msgType: MsgStartPeriod,
			valid:   true,
		},
		{
			name:    "StartInstance is valid",
			msgType: MsgStartInstance,
			valid:   true,
		},
		{
			name:    "Rollback is valid",
			msgType: MsgRollback,
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
				Payload: &pb.Message_StartPeriod{
					StartPeriod: &pb.StartPeriod{
						PeriodId:         1,
						SuperblockNumber: 10,
					},
				},
			},
			expected: MsgStartPeriod,
			valid:    true,
		},
		{
			name: "StartInstance message is correctly classified",
			msg: &pb.Message{
				Payload: &pb.Message_StartInstance{
					StartInstance: &pb.StartInstance{
						InstanceId:     []byte("xtid"),
						PeriodId:       1,
						SequenceNumber: 10,
						XtRequest: &pb.XTRequest{
							TransactionRequests: []*pb.TransactionRequest{
								{
									Transaction: [][]byte{[]byte("tx1")},
									ChainId:     10,
								},
							},
						},
					},
				},
			},
			expected: MsgStartInstance,
			valid:    true,
		},
		{
			name: "Rollback message is correctly classified",
			msg: &pb.Message{
				Payload: &pb.Message_Rollback{
					Rollback: &pb.Rollback{
						PeriodId:                      1,
						LastFinalizedSuperblockNumber: 10,
						LastFinalizedSuperblockHash:   []byte("hash"),
					},
				},
			},
			expected: MsgRollback,
			valid:    true,
		},
		{
			name: "non-SBCP message (Vote) returns invalid",
			msg: &pb.Message{
				Payload: &pb.Message_Vote{
					Vote: &pb.Vote{
						InstanceId: []byte("instance"),
						ChainId:    10,
						Vote:       true,
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
						TransactionRequests: []*pb.TransactionRequest{
							{
								ChainId:     10,
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
			name: "StartPeriod is SBCP message",
			msg: &pb.Message{
				Payload: &pb.Message_StartPeriod{
					StartPeriod: &pb.StartPeriod{},
				},
			},
			isSBCP: true,
		},
		{
			name: "StartInstance is SBCP message",
			msg: &pb.Message{
				Payload: &pb.Message_StartInstance{
					StartInstance: &pb.StartInstance{
						InstanceId:     []byte("xtid"),
						PeriodId:       1,
						SequenceNumber: 10,
						XtRequest:      &pb.XTRequest{},
					},
				},
			},
			isSBCP: true,
		},
		{
			name: "Rollback is SBCP message",
			msg: &pb.Message{
				Payload: &pb.Message_Rollback{
					Rollback: &pb.Rollback{
						PeriodId: 10,
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
			name: "MailboxMessage is not SBCP message",
			msg: &pb.Message{
				Payload: &pb.Message_MailboxMessage{
					MailboxMessage: &pb.MailboxMessage{},
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
			MsgStartPeriod,
			MsgStartInstance,
			MsgRollback,
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
			MsgStartPeriod,
			MsgStartInstance,
			MsgRollback,
		}

		for _, msgType := range validTypes {
			assert.True(t, msgType.IsValid(), "message type %s should be valid", msgType.String())
		}
	})
}
