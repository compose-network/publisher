package publisher

import (
	"encoding/hex"

	"github.com/ssvlabs/rollup-shared-publisher/internal/consensus"
	pb "github.com/ssvlabs/rollup-shared-publisher/internal/proto"
)

// handleVote processes vote messages from sequencers.
func (p *Publisher) handleVote(from string, vote *pb.Vote) error {
	chainID := hex.EncodeToString(vote.SenderChainId)

	log := p.log.With().
		Str("from", from).
		Uint32("xt_id", vote.XtId).
		Str("chain", chainID).
		Bool("vote", vote.Vote).
		Logger()

	log.Debug().Msg("Received vote")

	decision, err := p.coordinator.RecordVote(vote.XtId, chainID, vote.Vote)
	if err != nil {
		log.Error().Err(err).Msg("Failed to record vote")
		return err
	}

	if decision != consensus.StateUndecided {
		log.Info().
			Str("decision", decision.String()).
			Msg("Transaction decided")
	}

	return nil
}
