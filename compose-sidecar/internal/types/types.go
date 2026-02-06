package types

import (
	"time"

	"github.com/compose-network/compose-sdk/protocol"
	"github.com/compose-network/specs/compose"
	"github.com/compose-network/specs/compose/proto"
	"github.com/ethereum/go-ethereum/core/types"
)

// PendingXT represents a cross-chain transaction awaiting coordination.
// This type uses proto.MailboxMessage for CIRC messaging and is sidecar-specific.
type PendingXT struct {
	ID           string
	InstanceID   []byte
	PeriodID     compose.PeriodID
	SequenceNum  compose.SequenceNumber
	Transactions map[compose.ChainID][]*types.Transaction
	RawTxs       map[compose.ChainID][][]byte
	ChainStates  map[compose.ChainID]*protocol.ChainState
	CreatedAt    time.Time
	SimulatedAt  time.Time
	DecidedAt    time.Time
	Decision     *bool
	VoteSent     bool

	OriginChain compose.ChainID
	OriginSeq   compose.SequenceNumber
	LocalVote   *bool
	PeerVotes   map[compose.ChainID]bool

	LockedChains   map[compose.ChainID]bool
	StateOverrides map[compose.ChainID]map[string]any

	PendingMailbox   []*proto.MailboxMessage
	SentMailbox      []*proto.MailboxMessage
	Dependencies     []protocol.CrossRollupDependency
	FulfilledDeps    []protocol.CrossRollupDependency
	OutboundMessages []protocol.CrossRollupMessage
	PutInboxTxs      []*types.Transaction
	InFlightChains   map[compose.ChainID]bool
	DeliveredChains  map[compose.ChainID]bool
}
