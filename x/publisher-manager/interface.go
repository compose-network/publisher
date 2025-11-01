package publishermanager

import (
	"context"

	pb "github.com/compose-network/specs/compose/proto"
)

// PublisherManager orchestrates SBCP and SCP components for the publisher.
type PublisherManager interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	HandleMessage(ctx context.Context, from string, msg *pb.Message) error
	QueueStats(ctx context.Context) (int, error)
}
