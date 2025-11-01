package queue

import (
	"context"
)

type XTRequestQueue interface {
	Enqueue(ctx context.Context, request *QueuedXTRequest) error
	Dequeue(ctx context.Context) (*QueuedXTRequest, error)
	Peek(ctx context.Context) (*QueuedXTRequest, error)
	Size(ctx context.Context) (int, error)
	RemoveExpired(ctx context.Context) (int, error)
	Requeue(ctx context.Context, request *QueuedXTRequest) error
	Config() Config
}
