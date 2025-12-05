package handlers

import (
	"context"
	"fmt"

	"github.com/compose-network/publisher/x/consensus"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/rs/zerolog"
)

// SBCPHandler handles Superblock-specific messages
type SBCPHandler struct {
	consensusCoord consensus.Coordinator
	log            zerolog.Logger
}

func NewSBCPHandler(
	consensusCoord consensus.Coordinator,
	log zerolog.Logger,
) *SBCPHandler {
	return &SBCPHandler{
		consensusCoord: consensusCoord,
		log:            log,
	}
}

func (h *SBCPHandler) CanHandle(msg *pb.Message) bool {
	switch msg.Payload.(type) {
	case *pb.Message_StartPeriod,
		*pb.Message_MailboxMessage:
		return true
	default:
		return false
	}
}

func (h *SBCPHandler) Handle(ctx context.Context, from string, msg *pb.Message) error {
	h.log.Debug().
		Str("from", from).
		Str("type", fmt.Sprintf("%T", msg.Payload)).
		Msg("SBCP handler processing")

	switch payload := msg.Payload.(type) {
	case *pb.Message_MailboxMessage:
		h.log.Debug().
			Str("from", from).
			Str("source", fmt.Sprintf("%x", payload.MailboxMessage.SourceChain)).
			Str("dest", fmt.Sprintf("%x", payload.MailboxMessage.DestinationChain)).
			Str("instanceID_id", string(payload.MailboxMessage.InstanceId)).
			Msg("MailboxMessage message observed")

		return h.consensusCoord.RecordMailboxMessage(payload.MailboxMessage)

	case *pb.Message_StartPeriod:
		// Sequencers don't send this, but handle for completeness
		return nil

	case *pb.Message_StartInstance:
		// Handle if sequencer echoes back
		return nil

	default:
		return fmt.Errorf("SBCP handler cannot process %T", msg.Payload)
	}
}
