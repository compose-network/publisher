package consensus

import (
	"context"
	"time"

	"github.com/compose-network/specs/compose"
	"github.com/ethereum/go-ethereum/core/types"

	pb "github.com/compose-network/specs/compose/proto"
)

// Coordinator defines the consensus coordinator interface
type Coordinator interface {
	// Transaction lifecycle
	StartTransaction(ctx context.Context, instanceID compose.InstanceID, from string, xtReq *pb.XTRequest) error
	RecordVote(instanceID compose.InstanceID, chainID compose.ChainID, vote bool) (DecisionState, error)
	RecordDecision(instanceID compose.InstanceID, decision bool) error
	GetTransactionState(instanceID compose.InstanceID) (DecisionState, error)
	GetActiveTransactions() []compose.InstanceID
	GetState(instanceID compose.InstanceID) (*TwoPCState, bool)

	// Mailbox message handling
	RecordMailboxMessage(mailboxMessage *pb.MailboxMessage) error
	ConsumeMailboxMessage(instanceID compose.InstanceID, sourceChainID compose.ChainID) (*pb.MailboxMessage, error)

	// Callbacks
	SetStartCallback(fn StartFn)
	SetVoteCallback(fn VoteFn)
	SetDecisionCallback(fn DecisionFn)
	SetBlockCallback(fn BlockFn)

	// OnBlockCommitted is called by the execution layer when a new L2 block is committed and available
	// Implementations should gather committed xTs and trigger any registered BlockFn callback
	OnBlockCommitted(ctx context.Context, block *types.Block) error

	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Callback function types
type StartFn func(ctx context.Context, from string, xtReq *pb.XTRequest) error
type VoteFn func(ctx context.Context, instanceID compose.InstanceID, vote bool) error
type DecisionFn func(ctx context.Context, instanceID compose.InstanceID, decision bool) error

// BlockFn sends a block plus committed xTs to the SP layer
type BlockFn func(ctx context.Context, block *types.Block, instanceIDs []compose.InstanceID) error

// Config holds coordinator configuration
type Config struct {
	NodeID   string
	IsLeader bool
	Timeout  time.Duration
	Role     Role
}

// DefaultConfig returns sensible defaults
func DefaultConfig(nodeID string) Config {
	return Config{
		NodeID:   nodeID,
		IsLeader: true,
		Timeout:  time.Minute,
		Role:     Leader,
	}
}
