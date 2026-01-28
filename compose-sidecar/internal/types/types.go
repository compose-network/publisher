// Package types defines sidecar-specific types that depend on protobuf.
package types

import (
	"time"

	"github.com/compose-network/compose-sdk/protocol"
	"github.com/compose-network/specs/compose/proto"
	"github.com/ethereum/go-ethereum/core/types"
)

// PendingXT represents a cross-chain transaction awaiting coordination.
// This type uses proto.MailboxMessage for CIRC messaging and is sidecar-specific.
type PendingXT struct {
	ID           string
	InstanceID   []byte
	PeriodID     uint64
	SequenceNum  uint64
	Transactions map[uint64]*types.Transaction
	RawTxs       map[uint64][]byte
	ChainStates  map[uint64]*protocol.ChainState
	CreatedAt    time.Time
	SimulatedAt  time.Time
	DecidedAt    time.Time
	Decision     *bool
	VoteSent     bool

	IsLeader    bool
	LeaderChain uint64
	LocalVote   *bool
	PeerVotes   map[uint64]bool

	PendingMailbox   []*proto.MailboxMessage
	SentMailbox      []*proto.MailboxMessage
	Dependencies     []protocol.CrossRollupDependency
	FulfilledDeps    []protocol.CrossRollupDependency
	OutboundMessages []protocol.CrossRollupMessage
	PutInboxTxs      []*types.Transaction
	DeliveredChains  map[uint64]bool
}
