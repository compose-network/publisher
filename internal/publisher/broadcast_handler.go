package publisher

import (
	"context"

	pb "github.com/ssvlabs/rollup-shared-publisher/internal/proto"
)

// broadcastDecision broadcasts 2PC decisions to all sequencers.
func (p *Publisher) broadcastDecision(ctx context.Context, xtID uint32, decision bool) error {
	decided := &pb.Decided{
		XtId:     xtID,
		Decision: decision,
	}

	msg := &pb.Message{
		SenderId: "shared-publisher",
		Payload: &pb.Message_Decided{
			Decided: decided,
		},
	}

	p.log.Info().
		Uint32("xt_id", xtID).
		Bool("decision", decision).
		Msg("Broadcasting 2PC decision")

	err := p.server.Broadcast(ctx, msg, "")
	if err != nil {
		p.log.Error().
			Err(err).
			Uint32("xt_id", xtID).
			Bool("decision", decision).
			Msg("Failed to broadcast 2PC decision")
	}

	if !decision {
		p.activeTxs.Delete(xtID)
	}

	return err
}
