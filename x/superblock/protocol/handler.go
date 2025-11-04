package protocol

import (
	"context"
	"encoding/hex"
	"fmt"

	pb "github.com/compose-network/publisher/proto/rollup/v1"
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

	// High-level envelope log for any SBCP message
	h.log.Debug().
		Str("from", from).
		Str("message_type", msgType.String()).
		Msg("Handling SBCP message")

	// Add verbose, message-specific context to help trace xTs/txs
	switch msgType {
	case MsgStartSC:
		sc := msg.GetStartSc()
		if sc != nil {
			xtIDHex := hex.EncodeToString(sc.GetXtId())
			txCount := 0
			if sc.GetXtRequest() != nil {
				txCount = len(sc.GetXtRequest().GetTransactions())
			}
			h.log.Info().
				Str("from", from).
				Uint64("slot", sc.GetSlot()).
				Uint64("xt_sequence", sc.GetXtSequenceNumber()).
				Str("xt_id", xtIDHex).
				Int("transactions", txCount).
				Msg("SBCP StartSC received")
		}
	case MsgRequestSeal:
		rs := msg.GetRequestSeal()
		if rs != nil {
			included := bytesSliceToHex(rs.GetIncludedXts())
			h.log.Info().
				Str("from", from).
				Uint64("slot", rs.GetSlot()).
				Int("included_xts_count", len(included)).
				Strs("included_xts", included).
				Msg("SBCP RequestSeal received")
		}
	case MsgL2Block:
		lb := msg.GetL2Block()
		if lb != nil {
			included := bytesSliceToHex(lb.GetIncludedXts())
			h.log.Info().
				Str("from", from).
				Uint64("slot", lb.GetSlot()).
				Str("chain_id", hex.EncodeToString(lb.GetChainId())).
				Uint64("block_number", lb.GetBlockNumber()).
				Str("block_hash", hex.EncodeToString(lb.GetBlockHash())).
				Int("included_xts_count", len(included)).
				Strs("included_xts", included).
				Msg("SBCP L2Block received")
		}
	case MsgStartSlot:
		ss := msg.GetStartSlot()
		if ss != nil {
			h.log.Info().
				Str("from", from).
				Uint64("slot", ss.GetSlot()).
				Uint64("next_superblock_number", ss.GetNextSuperblockNumber()).
				Int("l2_block_requests", len(ss.GetL2BlocksRequest())).
				Msg("SBCP StartSlot received")
		}
	case MsgRollBackAndStartSlot:
		rb := msg.GetRollBackAndStartSlot()
		if rb != nil {
			h.log.Warn().
				Str("from", from).
				Uint64("current_slot", rb.GetCurrentSlot()).
				Uint64("next_superblock_number", rb.GetNextSuperblockNumber()).
				Int("l2_block_requests", len(rb.GetL2BlocksRequest())).
				Msg("SBCP RollBackAndStartSlot received")
		}
	}

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
	case MsgStartSlot:
		return h.validator.ValidateStartSlot(msg.GetStartSlot())
	case MsgRequestSeal:
		return h.validator.ValidateRequestSeal(msg.GetRequestSeal())
	case MsgL2Block:
		return h.validator.ValidateL2Block(msg.GetL2Block())
	case MsgStartSC:
		return h.validator.ValidateStartSC(msg.GetStartSc())
	case MsgRollBackAndStartSlot:
		return h.validator.ValidateRollBackAndStartSlot(msg.GetRollBackAndStartSlot())
	default:
		return fmt.Errorf("no validator for message type %s", msgType)
	}
}

// handleMessage routes the message to the appropriate handler
func (h *handler) handleMessage(ctx context.Context, from string, msgType MessageType, msg *pb.Message) error {
	switch msgType {
	case MsgStartSlot:
		return h.messageHandler.HandleStartSlot(ctx, from, msg.GetStartSlot())
	case MsgRequestSeal:
		return h.messageHandler.HandleRequestSeal(ctx, from, msg.GetRequestSeal())
	case MsgL2Block:
		return h.messageHandler.HandleL2Block(ctx, from, msg.GetL2Block())
	case MsgStartSC:
		return h.messageHandler.HandleStartSC(ctx, from, msg.GetStartSc())
	case MsgRollBackAndStartSlot:
		return h.messageHandler.HandleRollBackAndStartSlot(ctx, from, msg.GetRollBackAndStartSlot())
	default:
		return fmt.Errorf("no handler for message type %s", msgType)
	}
}

// bytesSliceToHex converts a slice of byte-slices ([][]byte) into
// their lower-hex string representation for structured logging.
func bytesSliceToHex(items [][]byte) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, b := range items {
		if len(b) == 0 {
			continue
		}
		out = append(out, hex.EncodeToString(b))
	}
	return out
}
