package publisher

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	pb "github.com/compose-network/publisher/proto/rollup/v1"
)

// HandlerFunc is a function that handles a specific message type
type HandlerFunc func(ctx context.Context, from string, msg *pb.Message) error

// MessageRouter routes messages to registered handlers based on message type
type MessageRouter interface {
	// Register registers a handler for a specific message payload type
	Register(payloadType reflect.Type, handler HandlerFunc)

	// Unregister removes a handler for a specific message payload type
	Unregister(payloadType reflect.Type)

	// Route dispatches a message to the appropriate handler
	Route(ctx context.Context, from string, msg *pb.Message) error

	// GetHandlers returns a copy of current handler registrations for inspection
	GetHandlers() map[reflect.Type]string
}

// messageRouter implements MessageRouter with thread-safe handler registration
type messageRouter struct {
	mu       sync.RWMutex
	handlers map[reflect.Type]HandlerFunc
}

// NewMessageRouter creates a new message router
func NewMessageRouter() MessageRouter {
	return &messageRouter{
		handlers: make(map[reflect.Type]HandlerFunc),
	}
}

// Register registers a handler for a specific message payload type
func (r *messageRouter) Register(payloadType reflect.Type, handler HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[payloadType] = handler
}

// Unregister removes a handler for a specific message payload type
func (r *messageRouter) Unregister(payloadType reflect.Type) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.handlers, payloadType)
}

// Route dispatches a message to the appropriate handler
func (r *messageRouter) Route(ctx context.Context, from string, msg *pb.Message) error {
	if msg.Payload == nil {
		return fmt.Errorf("message payload is nil")
	}

	payloadType := reflect.TypeOf(msg.Payload)

	r.mu.RLock()
	handler, exists := r.handlers[payloadType]
	r.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no handler registered for message type: %s", payloadType)
	}

	return handler(ctx, from, msg)
}

// GetHandlers returns a map of registered types to their handler names for debugging
func (r *messageRouter) GetHandlers() map[reflect.Type]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[reflect.Type]string, len(r.handlers))
	for t := range r.handlers {
		result[t] = t.String()
	}
	return result
}

// Helper functions for common message types
var (
	XTRequestType = reflect.TypeOf((*pb.Message_XtRequest)(nil))
	VoteType      = reflect.TypeOf((*pb.Message_Vote)(nil))
	BlockType     = reflect.TypeOf((*pb.Message_Block)(nil))
	DecidedType   = reflect.TypeOf((*pb.Message_Decided)(nil))
	L2BlockType   = reflect.TypeOf((*pb.Message_L2Block)(nil))
)
