package sequencer

import (
	"context"

	"github.com/compose-network/publisher/x/superblock/protocol"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/rs/zerolog"
)

// sbcpHandler implements the protocol.MessageHandler interface for SBCP messages
type sbcpHandler struct {
	coordinator *SequencerCoordinator
	log         zerolog.Logger
}

// NewSBCPHandler creates a new SBCP message handler
func NewSBCPHandler(coordinator *SequencerCoordinator, log zerolog.Logger) protocol.MessageHandler {
	return &sbcpHandler{
		coordinator: coordinator,
		log:         log.With().Str("component", "sbcp_handler").Logger(),
	}
}

// HandleStartPeriod processes StartPeriod messages
func (h *sbcpHandler) HandleStartPeriod(ctx context.Context, from string, startPeriod *pb.StartPeriod) error {
	h.log.Info().
		Str("from", from).
		Uint64("period_id", startPeriod.GetPeriodId()).
		Uint64("superblock_nr", startPeriod.GetSuperblockNumber()).
		Msg("Processing StartSC message")

	return h.coordinator.handleStartPeriod(ctx, from, startPeriod)
}

// HandleStartInstance processes StartInstance messages (cross-chain transaction initiation)
func (h *sbcpHandler) HandleStartInstance(ctx context.Context, from string, startInstance *pb.StartInstance) error {
	h.log.Info().
		Str("from", from).
		Uint64("sequence_number", startInstance.SequenceNumber).
		Msg("Processing StartSC message")

	return h.coordinator.handleStartInstance(ctx, from, startInstance)
}

// HandleRollback processes rollback messages
func (h *sbcpHandler) HandleRollback(ctx context.Context, from string, rb *pb.Rollback) error {
	h.log.Info().
		Str("from", from).
		Uint64("last_finalized_superblock_number", rb.LastFinalizedSuperblockNumber).
		Msg("Processing Rollback message")

	return h.coordinator.handleRollback(ctx, from, rb)
}
