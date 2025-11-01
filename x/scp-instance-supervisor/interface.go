package scpsupervisor

import (
	"context"

	"github.com/compose-network/publisher/x/superblock/queue"
	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/compose-network/specs/compose/scp"
	"github.com/rs/zerolog"
)

// SCPSupervisor manages multiple SCP instances.
// The supervisor holds an OnFinalizeHook that is called whenever an instance finalizes.
type SCPSupervisor interface {
	// StartInstance attempts to start a new SCP instance (with SCPFactory), returning an error if an instance with the same ID is already active.
	StartInstance(ctx context.Context, queued *queue.QueuedXTRequest, instance compose.Instance) error
	// HandleVote processes an incoming vote for an active SCP instance.
	HandleVote(ctx context.Context, vote *pb.Vote) error
	// History provides a list of completed SCP instances. Note that this list is cleaned up over time.
	History() []CompletedInstance
	SetOnFinalizeHook(OnFinalizeHook)
	// Stop stops the scpSupervisor, best-effort finalizing active instances.
	Stop(ctx context.Context) error
}

// OnFinalizeHook is called when an SCP instance finalizes.
type OnFinalizeHook func(instance compose.Instance)

// SCPFactory is used to create a new SCP publisher instance for the provided compose.Instance.
type SCPFactory func(instance compose.Instance, network scp.PublisherNetwork, logger zerolog.Logger) (scp.PublisherInstance, error)
