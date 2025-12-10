package handlers

import (
	"context"
	"fmt"

	"github.com/compose-network/publisher/x/superblock/sequencer"
	pb "github.com/compose-network/specs/compose/proto"
)

type XTInterceptor struct {
	coordinator sequencer.Coordinator
	trackFn     func(string) // Callback to track slot-managed XTs
	fallback    func(context.Context, string, *pb.Message) error
}

func NewXTInterceptor(coordinator sequencer.Coordinator, trackFn func(string)) *XTInterceptor {
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
	payload, ok := msg.Payload.(*pb.Message_XtRequest)
	if !ok {
		return fmt.Errorf("invalid message type for XTInterceptor")
	}

	xtReq := payload.XtRequest

	i.coordinator.SubmitXTRequest(ctx, from, xtReq)

	if i.fallback != nil {
		return i.fallback(ctx, from, msg)
	}

	return fmt.Errorf("cannot process XTRequest")
}
