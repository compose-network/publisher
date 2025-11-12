package handlers

import (
	"context"
	"fmt"

	pb "github.com/compose-network/publisher/proto/rollup/v1"
	"github.com/compose-network/publisher/x/superblock"
)

type XTInterceptor struct {
	coordinator *superblock.Coordinator
	trackFn     func(string) // Callback to track slot-managed XTs
	fallback    func(context.Context, string, *pb.Message) error
}

func NewXTInterceptor(coordinator *superblock.Coordinator, trackFn func(string)) *XTInterceptor {
	return &XTInterceptor{
		coordinator: coordinator,
		trackFn:     trackFn,
	}
}

func (i *XTInterceptor) SetFallback(f func(context.Context, string, *pb.Message) error) {
	i.fallback = f
}

func (i *XTInterceptor) CanHandle(msg *pb.Message) bool {
	_, ok := msg.Payload.(*pb.Message_XtRequest)
	return ok
}

func (i *XTInterceptor) Handle(ctx context.Context, from string, msg *pb.Message) error {
	payload := msg.Payload.(*pb.Message_XtRequest)
	xtReq := payload.XtRequest
	xtID, _ := xtReq.XtID()

	currentSlot := i.coordinator.GetCurrentSlot()
	slotState := i.coordinator.GetSlotState()

	if currentSlot > 0 {
		i.coordinator.Logger().Info().
			Str("xt_id", xtID.Hex()).
			Uint64("slot", currentSlot).
			Str("slot_state", slotState.String()).
			Msg("Queueing XT for SBCP processing")

		if i.trackFn != nil {
			i.trackFn(xtID.Hex())
		}

		return i.coordinator.SubmitXTRequest(ctx, from, xtReq)
	}

	if i.fallback != nil {
		i.coordinator.Logger().Debug().
			Str("xt_id", xtID.Hex()).
			Msg("Passing XT to fallback (no active slot)")
		return i.fallback(ctx, from, msg)
	}

	return fmt.Errorf("cannot process XTRequest: no active slot and no fallback")
}
