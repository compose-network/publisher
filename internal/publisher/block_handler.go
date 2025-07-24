package publisher

import (
	"context"
	"encoding/hex"

	"github.com/ssvlabs/rollup-shared-publisher/pkg/consensus"
	pb "github.com/ssvlabs/rollup-shared-publisher/pkg/proto"
)

// handleBlock processes block submissions from sequencers.
func (p *Publisher) handleBlock(ctx context.Context, from string, block *pb.Block) error {
	chainID := hex.EncodeToString(block.ChainId)

	log := p.log.With().
		Str("from", from).
		Str("chain", chainID).
		Int("included_xts", len(block.IncludedXtIds)).
		Logger()

	log.Info().Msg("Received block")

	for _, xtID := range block.IncludedXtIds {
		state, err := p.coordinator.GetTransactionState(xtID)
		if err != nil {
			continue
		}

		if state == consensus.StateCommit {
			xtIDStr := xtID.Hex()
			p.activeTxs.Delete(xtIDStr)
			log.Debug().
				Str("xt_id", xtIDStr).
				Msg("Confirmed xT inclusion in block")
		}
	}

	connections := p.server.GetConnections()
	recipientCount := len(connections) - 1

	if recipientCount > 0 {
		if err := p.server.Broadcast(ctx, &pb.Message{
			SenderId: "shared-publisher",
			Payload: &pb.Message_Block{
				Block: block,
			},
		}, from); err != nil {
			log.Error().Err(err).Msg("Failed to broadcast block")
			return err
		}

		p.broadcastCnt.Add(1)
	}

	return nil
}
