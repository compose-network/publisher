package publisher

import (
	"context"
	"fmt"

	pb "github.com/ssvlabs/rollup-shared-publisher/pkg/proto"
)

// handleXTRequest handles cross-chain transaction requests.
func (p *Publisher) handleXTRequest(ctx context.Context, from string, msg *pb.Message, xtReq *pb.XTRequest) error {
	xtID, err := xtReq.XtID()
	if err != nil {
		return fmt.Errorf("failed to generate xtID: %w", err)
	}

	log := p.log.With().
		Str("from", from).
		Str("sender_id", msg.SenderId).
		Str("xt_id", xtID.Hex()).
		Int("tx_count", len(xtReq.Transactions)).
		Logger()

	log.Info().Msg("Received xT request, initiating 2PC")

	participantChains := xtReq.ChainIDs()
	if len(participantChains) == 0 {
		return fmt.Errorf("no participating chains found")
	}

	if err := p.coordinator.StartTransaction(from, xtReq); err != nil {
		log.Error().Err(err).Msg("Failed to start 2PC transaction")
		return err
	}

	p.activeTxs.Store(xtID.Hex(), xtReq)

	p.mu.Lock()
	for chainID := range participantChains {
		if !p.chains[chainID] {
			p.chains[chainID] = true
			p.metrics.RecordUniqueChain(chainID)
		}
	}
	p.mu.Unlock()

	p.metrics.RecordCrossChainTransaction(len(xtReq.Transactions))

	if err := p.server.Broadcast(ctx, msg, from); err != nil {
		log.Error().Err(err).Msg("Failed to broadcast xT request")
		return err
	}

	log.Info().
		Int("participating_chains", len(participantChains)).
		Msg("Successfully initiated 2PC and broadcast xT request")

	return nil
}
