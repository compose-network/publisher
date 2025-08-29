package publisher

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/ssvlabs/rollup-shared-publisher/x/consensus"

	pb "github.com/ssvlabs/rollup-shared-publisher/proto/rollup/v1"
)

// HandleMessage routes messages to the registered handlers
func (p *publisher) HandleMessage(ctx context.Context, from string, msg *pb.Message) error {
	p.msgCount.Add(1)

	p.log.Info().
		Str("from", from).
		Str("sender_id", msg.SenderId).
		Str("msg_type", fmt.Sprintf("%T", msg.Payload)).
		Msg("Handling message")

	return p.router.Route(ctx, from, msg)
}

// handleXTRequest handles cross-chain transaction requests
func (p *publisher) handleXTRequest(ctx context.Context, from string, msg *pb.Message) error {
	payload, ok := msg.Payload.(*pb.Message_XtRequest)
	if !ok {
		return fmt.Errorf("invalid payload type for XTRequest")
	}
	xtReq := payload.XtRequest
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

	if err := p.consensus.StartTransaction(from, xtReq); err != nil {
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

	if err := p.transport.Broadcast(ctx, msg, ""); err != nil {
		log.Error().Err(err).Msg("Failed to broadcast xT request")
		return err
	}

	p.broadcastCnt.Add(1)

	log.Info().
		Int("participating_chains", len(participantChains)).
		Msg("Successfully initiated 2PC and broadcast xT request")

	return nil
}

// handleVote processes vote messages from sequencers
func (p *publisher) handleVote(ctx context.Context, from string, msg *pb.Message) error {
	payload, ok := msg.Payload.(*pb.Message_Vote)
	if !ok {
		return fmt.Errorf("invalid payload type for Vote")
	}
	vote := payload.Vote
	chainID := consensus.ChainKeyBytes(vote.SenderChainId)

	log := p.log.With().
		Str("from", from).
		Str("xt_id", vote.XtId.Hex()).
		Str("chain", chainID).
		Bool("vote", vote.Vote).
		Logger()

	log.Debug().Msg("Received vote")

	decision, err := p.consensus.RecordVote(vote.XtId, chainID, vote.Vote)
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

// handleBlock processes block submissions from sequencers
func (p *publisher) handleBlock(ctx context.Context, from string, msg *pb.Message) error {
	payload, ok := msg.Payload.(*pb.Message_Block)
	if !ok {
		return fmt.Errorf("invalid payload type for Block")
	}
	block := payload.Block
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
		state, err := p.consensus.GetTransactionState(xtID)
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
	return nil
}
