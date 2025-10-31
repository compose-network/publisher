package messenger

import (
	"context"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/compose-network/specs/compose/sbcp"
	"github.com/compose-network/specs/compose/scp"
)

// Messenger includes the functions to send messages required by the publisher spec
type Messenger interface {
	scp.PublisherNetwork
	sbcp.Messenger
}

// Broadcaster is used by the messenger to broadcast proto messages
type Broadcaster interface {
	// Broadcast sends msg to every connection except to "excludeID"
	Broadcast(ctx context.Context, msg *pb.Message, excludeID string) error
}
