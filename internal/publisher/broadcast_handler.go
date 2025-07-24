package publisher

import (
	"context"

	pb "github.com/ssvlabs/rollup-shared-publisher/pkg/proto"
)

// broadcastDecision broadcasts 2PC decisions to all sequencers.
func (p *Publisher) broadcastDecision(ctx context.Context, xtID *pb.XtID, decision bool) error {
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

	xtIDStr := xtID.Hex()
	p.log.Info().
		Str("xt_id", xtIDStr).
		Bool("decision", decision).
		Msg("Broadcasting 2PC decision")

	err := p.server.Broadcast(ctx, msg, "")
	if err != nil {
		p.log.Error().
			Err(err).
			Str("xt_id", xtIDStr).
			Bool("decision", decision).
			Msg("Failed to broadcast 2PC decision")
	}

	if !decision {
		p.activeTxs.Delete(xtIDStr)
	}

	return err
}
