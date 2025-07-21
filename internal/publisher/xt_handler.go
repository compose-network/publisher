package publisher

import (
	"context"
	"encoding/hex"
	"fmt"

	pb "github.com/ssvlabs/rollup-shared-publisher/internal/proto"
)

// handleXTRequest handles cross-chain transaction requests.
func (p *Publisher) handleXTRequest(ctx context.Context, from string, msg *pb.Message, req *pb.XTRequest) error {
	xtID := p.nextXTID.Add(1)

	log := p.log.With().
		Str("from", from).
		Str("sender_id", msg.SenderId).
		Uint32("xt_id", xtID).
		Int("tx_count", len(req.Transactions)).
		Logger()

	log.Info().Msg("Received xT request, initiating 2PC")

	participatingChains := p.extractParticipatingChains(req)
	if len(participatingChains) == 0 {
		return fmt.Errorf("no participating chains found")
	}

	if err := p.coordinator.StartTransaction(xtID, req, participatingChains); err != nil {
		log.Error().Err(err).Msg("Failed to start 2PC transaction")
		return err
	}

	p.activeTxs.Store(xtID, req)

	p.mu.Lock()
	for chainID := range participatingChains {
		if !p.chains[chainID] {
			p.chains[chainID] = true
			p.metrics.RecordUniqueChain(chainID)
		}
	}
	p.mu.Unlock()

	p.metrics.RecordCrossChainTransaction(len(req.Transactions))

	if err := p.server.Broadcast(ctx, msg, from); err != nil {
		log.Error().Err(err).Msg("Failed to broadcast xT request")
		return err
	}

	log.Info().
		Int("participating_chains", len(participatingChains)).
		Msg("Successfully initiated 2PC and broadcast xT request")

	return nil
}

// extractParticipatingChains extracts unique chain IDs from the XTRequest.
func (p *Publisher) extractParticipatingChains(req *pb.XTRequest) map[string]struct{} {
	chains := make(map[string]struct{})
	for _, tx := range req.Transactions {
		if tx != nil && len(tx.ChainId) > 0 {
			chainID := hex.EncodeToString(tx.ChainId)
			chains[chainID] = struct{}{}
		}
	}
	return chains
}
