// Package coordinator implements the cross-chain transaction coordination logic.
package coordinator

import (
	"context"
	"time"

	"github.com/compose-network/compose-sdk/protocol"
	"github.com/compose-network/specs/compose/proto"
)

// Coordinator defines the interface for cross-chain transaction coordination.
type Coordinator interface {
	// Start starts the coordinator.
	Start(ctx context.Context) error

	// Stop stops the coordinator gracefully.
	Stop(ctx context.Context) error

	// HandleBuilderPoll processes a poll request from a builder.
	HandleBuilderPoll(ctx context.Context, req *protocol.BuilderPollRequest) (*protocol.BuilderPollResponse, error)

	// SubmitXT submits a new cross-chain transaction for coordination.
	SubmitXT(ctx context.Context, id string, txs map[uint64][]byte) (string, error)

	// HandleStartInstance handles a StartInstance message from the publisher.
	HandleStartInstance(ctx context.Context, msg *proto.StartInstance) error

	// HandleStartPeriod handles a StartPeriod message from the publisher.
	HandleStartPeriod(ctx context.Context, periodID, superblockNum uint64) error

	// OnDecision handles a 2PC decision from the publisher.
	OnDecision(ctx context.Context, instanceID string, decision bool) error

	// HandleMailboxMessage handles an incoming mailbox message from a peer sidecar.
	HandleMailboxMessage(ctx context.Context, msg *proto.MailboxMessage) error

	// Cleanup removes completed XTs older than the given duration.
	Cleanup(maxAge time.Duration)

	// HandlePeerVote processes a vote from a peer sidecar (v2 standalone mode).
	HandlePeerVote(ctx context.Context, instanceID string, chainID uint64, vote bool) error

	// HandleForwardedXT processes an XT forwarded from another sidecar (v2 standalone mode).
	HandleForwardedXT(
		ctx context.Context,
		instanceID string,
		txs map[uint64][]byte,
		originChain uint64,
		originSeq uint64,
	) error

	// GetXTStatus retrieves the current status of a cross-chain transaction.
	GetXTStatus(ctx context.Context, instanceID string) (*XTStatusResponse, error)
}

// XTStatusResponse represents the response for an XT status query.
type XTStatusResponse struct {
	InstanceID string            `json:"instance_id"`
	Status     protocol.XTStatus `json:"status"`
	Decision   *bool             `json:"decision,omitempty"`
}

// Simulator defines the interface for transaction simulation.
type Simulator interface {
	// Simulate simulates a transaction on the given chain.
	Simulate(
		ctx context.Context,
		chainID uint64,
		tx []byte,
		stateOverrides map[string]interface{},
	) (*protocol.SimulationResult, error)

	// SimulateWithMailbox simulates a transaction with mailbox analysis.
	// Returns both the simulation result and detected mailbox operations.
	SimulateWithMailbox(
		ctx context.Context,
		chainID uint64,
		tx []byte,
		stateOverrides map[string]interface{},
		alreadySentMsgs []protocol.CrossRollupMessage,
		fulfilledDeps []protocol.CrossRollupDependency,
	) (*protocol.SimulationResult, error)
}

// PublisherClient defines the interface for publisher communication.
type PublisherClient interface {
	// Connect connects to the publisher.
	Connect(ctx context.Context) error

	// ConnectWithRetry attempts to connect with retries.
	ConnectWithRetry(ctx context.Context) error

	// Disconnect disconnects from the publisher.
	Disconnect(ctx context.Context) error

	// SendVote sends a vote for a cross-chain transaction.
	SendVote(ctx context.Context, instanceID []byte, vote bool) error

	// SendRaw sends a raw protobuf message to the publisher.
	SendRaw(ctx context.Context, data []byte) error

	// IsConnected returns whether the client is connected.
	IsConnected() bool

	// SetOnStart sets the callback for new XT starts.
	SetOnStart(fn StartCallback)

	// SetOnDecision sets the callback for decisions.
	SetOnDecision(fn VoteCallback)
}

// VoteCallback is called when a 2PC decision is received.
type VoteCallback func(ctx context.Context, instanceID string, decision bool) error

// StartCallback is called when a new cross-chain transaction starts (StartInstance).
type StartCallback func(ctx context.Context, msg *proto.StartInstance) error

// PeriodCallback is called when a new period starts (StartPeriod).
type PeriodCallback func(ctx context.Context, periodID, superblockNum uint64) error

// MailboxSender sends mailbox messages to peer sidecars.
type MailboxSender interface {
	// Send sends a mailbox message to the destination chain's sidecar.
	Send(ctx context.Context, destChainID uint64, msg *proto.MailboxMessage) error
}

// MailboxQueue is mailbox.Queue from SDK.
// PeerCoordinator is peer.Coordinator from SDK.
