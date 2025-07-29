package publisher

import (
	"encoding/hex"

	"github.com/ssvlabs/rollup-shared-publisher/pkg/consensus"
	pb "github.com/ssvlabs/rollup-shared-publisher/pkg/proto"
)

// handleBlock processes block submissions from sequencers.
func (p *Publisher) handleBlock(from string, block *pb.Block) {
	chainID := hex.EncodeToString(block.ChainId)

	log := p.log.With().
		Str("from", from).
		Str("chain", chainID).
		Int("included_xts", len(block.IncludedXtIds)).
		Int("block_data_size", len(block.BlockData)).
		Logger()

	log.Info().Msg("Received block")

	if len(block.IncludedXtIds) > 0 {
		xtIDs := make([]string, len(block.IncludedXtIds))
		for i, xtID := range block.IncludedXtIds {
			xtIDs[i] = xtID.Hex()
		}
		log.Info().
			Strs("included_xt_ids", xtIDs).
			Msg("Block contains cross-chain transactions")
	}

	for _, xtID := range block.IncludedXtIds {
		state, err := p.coordinator.GetTransactionState(xtID)
		if err != nil {
			log.Debug().
				Str("xt_id", xtID.Hex()).
				Err(err).
				Msg("Could not get transaction state")
			continue
		}

		if state == consensus.StateCommit {
			xtIDStr := xtID.Hex()
			p.activeTxs.Delete(xtIDStr)
			log.Info().
				Str("xt_id", xtIDStr).
				Str("state", state.String()).
				Msg("Confirmed xT inclusion in block")
		} else {
			log.Debug().
				Str("xt_id", xtID.Hex()).
				Str("state", state.String()).
				Msg("xT in block but not in commit state")
		}
	}
}
