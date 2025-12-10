package protocol

import (
	"fmt"

	pb "github.com/compose-network/specs/compose/proto"
)

// MessageType represents SBCP protocol message types
type MessageType int

const (
	_                MessageType = iota
	MsgStartInstance             // SP starts a cross-chain transaction
	MsgStartPeriod               // SP starts a cross-chain transaction
	MsgRollback                  // SP requests rollback and restart slot
)

// String returns a human-readable message type name
func (t MessageType) String() string {
	names := map[MessageType]string{
		MsgStartInstance: "StartInstance",
		MsgStartPeriod:   "StartPeriod",
		MsgRollback:      "Rollback",
	}

	if name, ok := names[t]; ok {
		return name
	}
	return fmt.Sprintf("Unknown(%d)", t)
}

// IsValid returns true if a message type is valid
func (t MessageType) IsValid() bool {
	return t >= MsgStartInstance && t <= MsgRollback
}

// ClassifyMessage returns SBCP message type from a protobuf message
func ClassifyMessage(msg *pb.Message) (MessageType, bool) {
	if msg == nil || msg.Payload == nil {
		return 0, false
	}

	switch msg.Payload.(type) {
	case *pb.Message_StartPeriod:
		return MsgStartPeriod, true
	case *pb.Message_StartInstance:
		return MsgStartInstance, true
	case *pb.Message_Rollback:
		return MsgRollback, true
	default:
		return 0, false
	}
}

// IsSBCPMessage returns true if the message belongs to SBCP protocol
func IsSBCPMessage(msg *pb.Message) bool {
	_, ok := ClassifyMessage(msg)
	return ok
}
