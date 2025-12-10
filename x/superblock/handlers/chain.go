package handlers

import (
	"context"
	"fmt"

	pb "github.com/compose-network/specs/compose/proto"
)

// messageHandler can process messages
type messageHandler interface {
	CanHandle(msg *pb.Message) bool
	Handle(ctx context.Context, from string, msg *pb.Message) error
}

// HandlerChain processes messages through multiple handlers
type HandlerChain struct {
	handlers []messageHandler
	fallback func(context.Context, string, *pb.Message) error
}

func NewHandlerChain() *HandlerChain {
	return &HandlerChain{
		handlers: make([]messageHandler, 0),
	}
}

func (c *HandlerChain) AddHandler(h messageHandler) {
	c.handlers = append(c.handlers, h)
}

func (c *HandlerChain) SetFallback(f func(context.Context, string, *pb.Message) error) {
	c.fallback = f
}

func (c *HandlerChain) Process(ctx context.Context, from string, msg *pb.Message) error {
	for _, handler := range c.handlers {
		if handler.CanHandle(msg) {
			return handler.Handle(ctx, from, msg)
		}
	}

	if c.fallback != nil {
		return c.fallback(ctx, from, msg)
	}

	return fmt.Errorf("no handler for message type: %T", msg.Payload)
}
