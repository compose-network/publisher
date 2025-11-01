package sbcpcontroller

import (
	"context"

	"github.com/compose-network/publisher/x/superblock/queue"
	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
)

// InstanceStarter starts an SCP instance using the granted SBCP instance descriptor.
type InstanceStarter interface {
	StartInstance(ctx context.Context, queued *queue.QueuedXTRequest, instance compose.Instance) error
}

// SBCPController exposes the SBCP coordination surface used by upper layers.
type SBCPController interface {
	EnqueueXTRequest(ctx context.Context, req *pb.XTRequest, from string) error
	TryProcessQueue(ctx context.Context) error
	OnNewPeriod(ctx context.Context) error
	NotifyInstanceDecided(ctx context.Context, instance compose.Instance) error
	AdvanceSettledState(superblockNumber compose.SuperblockNumber, superblockHash compose.SuperBlockHash) error
	ProofTimeout(ctx context.Context)
	SetInstanceStarter(starter InstanceStarter)
	Stop(ctx context.Context) error
}
