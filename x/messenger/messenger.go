package messenger

import (
	"context"

	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/rs/zerolog"
)

// messenger implements Messenger with the given broadcaster
type messenger struct {
	ctx         context.Context
	logger      zerolog.Logger
	broadcaster Broadcaster
}

func NewMessenger(
	ctx context.Context,
	logger zerolog.Logger,
	broadcaster Broadcaster,
) Messenger {
	return &messenger{
		ctx:         ctx,
		broadcaster: broadcaster,
		logger:      logger,
	}
}

// SendStartInstance broadcasts a StartInstance message
func (n *messenger) SendStartInstance(instance compose.Instance) {

	tr := make([]*pb.TransactionRequest, 0)
	for _, t := range instance.XTRequest.Transactions {
		tr = append(tr, &pb.TransactionRequest{
			ChainId:     uint64(t.ChainID),
			Transaction: t.Transactions,
		})
	}

	msg := &pb.Message{
		SenderId: "publisher",
		Payload: &pb.Message_StartInstance{
			StartInstance: &pb.StartInstance{
				InstanceId:     instance.ID[:],
				PeriodId:       uint64(instance.PeriodID),
				SequenceNumber: uint64(instance.SequenceNumber),
				XtRequest: &pb.XTRequest{
					TransactionRequests: tr,
				},
			},
		},
	}

	if err := n.broadcaster.Broadcast(n.ctx, msg, ""); err != nil {
		n.logger.Error().Err(err).Msg("Failed to broadcast StartInstance message")
	}
}

// SendDecided broadcasts a Decided message
func (n *messenger) SendDecided(instanceID compose.InstanceID, decided bool) {
	msg := &pb.Message{
		SenderId: "publisher",
		Payload: &pb.Message_Decided{
			Decided: &pb.Decided{
				InstanceId: instanceID[:],
				Decision:   decided,
			},
		},
	}

	if err := n.broadcaster.Broadcast(n.ctx, msg, ""); err != nil {
		n.logger.Error().Err(err).Msg("Failed to broadcast Decided message")
	}
}

// BroadcastStartPeriod broadcasts a StartPeriod message
func (n *messenger) BroadcastStartPeriod(periodID compose.PeriodID, targetSuperblockNumber compose.SuperblockNumber) {
	msg := &pb.Message{
		SenderId: "publisher",
		Payload: &pb.Message_StartPeriod{
			StartPeriod: &pb.StartPeriod{
				PeriodId:         uint64(periodID),
				SuperblockNumber: uint64(targetSuperblockNumber),
			},
		},
	}

	if err := n.broadcaster.Broadcast(n.ctx, msg, ""); err != nil {
		n.logger.Error().Err(err).Msg("Failed to broadcast StartPeriod message")
	}
}

// BroadcastRollback broadcasts a Rollback message
func (n *messenger) BroadcastRollback(periodID compose.PeriodID, superblockNumber compose.SuperblockNumber, superblockHash compose.SuperBlockHash) {
	msg := &pb.Message{
		SenderId: "publisher",
		Payload: &pb.Message_Rollback{
			Rollback: &pb.Rollback{
				PeriodId:                      uint64(periodID),
				LastFinalizedSuperblockNumber: uint64(superblockNumber),
				LastFinalizedSuperblockHash:   superblockHash[:],
			},
		},
	}

	if err := n.broadcaster.Broadcast(n.ctx, msg, ""); err != nil {
		n.logger.Error().Err(err).Msg("Failed to broadcast Rollback message")
	}
}
