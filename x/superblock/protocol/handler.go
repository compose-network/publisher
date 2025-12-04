package protocol

import (
	"context"
	"fmt"

	pb "github.com/compose-network/specs/compose/proto"
	"github.com/rs/zerolog"
)

// handler implements the SBCP protocol Handler interface
type handler struct {
	messageHandler MessageHandler
	validator      Validator
	log            zerolog.Logger
}

// NewHandler creates a new SBCP protocol handler
func NewHandler(messageHandler MessageHandler, validator Validator, log zerolog.Logger) Handler {
	return &handler{
		messageHandler: messageHandler,
		validator:      validator,
		log:            log.With().Str("protocol", "SBCP").Logger(),
	}
}

// CanHandle returns true if this handler can process the message.
func (h *handler) CanHandle(msg *pb.Message) bool {
	return IsSBCPMessage(msg)
}

// GetProtocolName returns the protocol name.
func (h *handler) GetProtocolName() string {
	return "SBCP"
}

// Handle processes SBCP protocol messages
func (h *handler) Handle(ctx context.Context, from string, msg *pb.Message) error {
	msgType, ok := ClassifyMessage(msg)
	if !ok {
		return fmt.Errorf("invalid or unsupported SBCP message from %s", from)
	}

	h.log.Debug().
		Str("from", from).
		Str("message_type", msgType.String()).
		Msg("Handling SBCP message")

	if h.validator == nil {
		return h.handleMessage(ctx, from, msgType, msg)
	}

	if err := h.validateMessage(msgType, msg); err != nil {
		return fmt.Errorf("validation failed for %s from %s: %w", msgType, from, err)
	}

	return h.handleMessage(ctx, from, msgType, msg)
}

// validateMessage validates the message based on its type
func (h *handler) validateMessage(msgType MessageType, msg *pb.Message) error {
	switch msgType {
	case MsgStartPeriod:
		return h.validator.ValidateStartPeriod(msg.GetStartPeriod())
	case MsgStartInstance:
		return h.validator.ValidateStartInstance(msg.GetStartInstance())
	case MsgRollback:
		return h.validator.ValidateRollback(msg.GetRollback())
	default:
		return fmt.Errorf("no validator for message type %s", msgType)
	}
}

// handleMessage routes the message to the appropriate handler
func (h *handler) handleMessage(ctx context.Context, from string, msgType MessageType, msg *pb.Message) error {
	switch msgType {
	case MsgStartPeriod:
		return h.messageHandler.HandleStartPeriod(ctx, from, msg.GetStartPeriod())
	case MsgStartInstance:
		return h.messageHandler.HandleStartInstance(ctx, from, msg.GetStartInstance())
	case MsgRollback:
		return h.messageHandler.HandleRollback(ctx, from, msg.GetRollback())
	default:
		return fmt.Errorf("no handler for message type %s", msgType)
	}
}
