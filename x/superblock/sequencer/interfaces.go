package sequencer

import (
	"context"

	"github.com/compose-network/publisher/x/consensus"
	"github.com/compose-network/publisher/x/transport"
	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
)

// MinerNotifier defines the interface for notifying miner about sequencer events
type MinerNotifier interface {
	NotifyStateChange(from, to State, slot uint64) error
}

// CoordinatorCallbacks defines callback functions for cross-component communication
type CoordinatorCallbacks struct {
	SendMailboxMessage func(ctx context.Context, circ *pb.MailboxMessage) error
	// SimulateAndVote runs local-chain simulation for the provided XT request
	// and returns whether the local transactions are ready to commit (vote=true)
	// or not (vote=false). This callback is used by the coordinator during
	// StartSC handling and is implemented by the host SDK (e.g., geth backend).
	SimulateAndVote func(ctx context.Context, xtReq *pb.XTRequest, instanceID compose.InstanceID) (bool, error)
	// CleanupAbortedTransaction is called when an SCP instance decides to abort,
	// allowing the execution layer to immediately remove staged transactions from
	// its pending pool. This ensures atomic exclude behavior when blocks are built
	// before RequestSeal arrives.
	CleanupAbortedTransaction func(ctx context.Context, instanceID compose.InstanceID) error
}

// BlockLifecycleManager handles block building lifecycle events
type BlockLifecycleManager interface {
	OnBlockBuildingStart(ctx context.Context, slot uint64) error
}

// TransactionManager handles transaction preparation and ordering
type TransactionManager interface {
	PrepareTransactionsForBlock(ctx context.Context, slot uint64) error
	GetOrderedTransactionsForBlock(ctx context.Context) ([]*pb.TransactionRequest, error)
}

// CallbackManager handles callback registration and miner notifications
type CallbackManager interface {
	SetCallbacks(callbacks CoordinatorCallbacks)
	SetMinerNotifier(notifier MinerNotifier)
}

// Coordinator defines the sequencer coordinator interface
type Coordinator interface {
	SubmitXTRequest(ctx context.Context, from string, request *pb.XTRequest) error
	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error

	// Message handling
	HandleMessage(ctx context.Context, from string, msg *pb.Message) error

	// State queries
	GetCurrentSlot() uint64
	GetState() State
	GetStats() map[string]interface{}
	GetActiveSCPInstanceCount() int

	// Consensus access
	Consensus() consensus.Coordinator
	Transport() transport.Transport

	// SDK access
	BlockLifecycleManager
	TransactionManager
	CallbackManager
}

// BlockBuilderInterface for L2 block construction
type BlockBuilderInterface interface {
	AddLocalTransaction(tx []byte) error
	AddSCPTransactions(xtID string, txs [][]byte, decision bool) error
	AddMailboxMessage(msg *pb.MailboxMessage) error
	GetDraftStats() map[string]interface{}
	Reset()
}

// StateMachineInterface for sequencer FSM
type StateMachineInterface interface {
	GetCurrentState() State
	GetCurrentSlot() uint64
	TransitionTo(newState State, slot uint64, reason string) error
	GetTransitions() []StateTransition
	Reset()
}

// MessageRouterInterface for routing messages
type MessageRouterInterface interface {
	Route(ctx context.Context, from string, msg *pb.Message) error
}

// SCPIntegrationInterface for SCP coordination
type SCPIntegrationInterface interface {
	HandleStartInstance(ctx context.Context, startSC *pb.StartInstance) error
	HandleDecision(instanceID compose.InstanceID, decision bool) error
	GetActiveContexts() map[string]*SCPContext
	ResetForSlot(slot uint64)
	GetIncludedXTsHex() []string
	GetLastDecidedSequenceNumber() (uint64, bool)
	GetActiveCount() int
}
