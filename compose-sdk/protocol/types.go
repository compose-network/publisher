package protocol

import (
	"encoding/json"
	"math/big"
	"time"

	"github.com/compose-network/specs/compose"
	"github.com/ethereum/go-ethereum/common"
)

// BuilderPollRequest represents a request from op-rbuilder at flashblock start.
type BuilderPollRequest struct {
	ChainID         compose.ChainID `json:"chain_id"`
	BlockNumber     uint64          `json:"block_number"`
	FlashblockIndex uint64          `json:"flashblock_index"`
	StateRoot       common.Hash     `json:"state_root"`
	Timestamp       uint64          `json:"timestamp"`
	GasLimit        uint64          `json:"gas_limit"`
	StateOverrides  json.RawMessage `json:"state_overrides,omitempty"`
}

// BuilderPollResponse represents the sidecar's response to a builder poll.
type BuilderPollResponse struct {
	Hold        bool                 `json:"hold"`
	PollAfterMs uint64               `json:"poll_after_ms,omitempty"`
	MaxHoldMs   uint64               `json:"max_hold_ms,omitempty"`
	Txs         []TransactionPayload `json:"transactions,omitempty"`
}

// TransactionPayload represents a transaction to be included by the builder.
type TransactionPayload struct {
	Raw        string `json:"raw"`
	Required   bool   `json:"required"`
	InstanceID string `json:"instance_id,omitempty"`
}

// ChainState represents the frozen state from a builder poll.
type ChainState struct {
	ChainID         compose.ChainID
	BlockNumber     uint64
	FlashblockIndex uint64
	StateRoot       common.Hash
	Timestamp       uint64
	GasLimit        uint64
	ReceivedAt      time.Time
	StateOverrides  json.RawMessage
}

// XTStatus represents the status of a cross-chain transaction.
type XTStatus string

const (
	XTStatusPending     XTStatus = "pending"
	XTStatusSimulating  XTStatus = "simulating"
	XTStatusWaitingCIRC XTStatus = "waiting_circ"
	XTStatusSimulated   XTStatus = "simulated"
	XTStatusVoted       XTStatus = "voted"
	XTStatusCommitted   XTStatus = "committed"
	XTStatusAborted     XTStatus = "aborted"
)

// Vote represents a vote in the 2PC protocol.
type Vote struct {
	InstanceID string
	ChainID    compose.ChainID
	Vote       bool
	Reason     string
}

// Decision represents the final decision of a cross-chain transaction.
type Decision struct {
	InstanceID string
	Commit     bool
	Reason     string
	DecidedAt  time.Time
}

// CrossRollupDependency represents a mailbox.read() requiring data from another chain.
type CrossRollupDependency struct {
	SourceChainID compose.ChainID
	DestChainID   compose.ChainID
	Sender        common.Address
	Receiver      common.Address
	SessionID     *big.Int
	Label         []byte
	RequiredData  bool
	IsInboxRead   bool
	Data          []byte // Fulfilled by CIRC message
}

// CrossRollupMessage represents a mailbox.write() sending data to another chain.
type CrossRollupMessage struct {
	SourceChainID compose.ChainID
	DestChainID   compose.ChainID
	Sender        common.Address
	Receiver      common.Address
	SessionID     *big.Int
	Data          []byte
	Label         []byte
	MessageType   string
	IsOutboxWrite bool
}

// SimulationResult represents the result of simulating a transaction.
type SimulationResult struct {
	ChainID          compose.ChainID
	Success          bool
	Error            string
	GasUsed          uint64
	StateChanges     map[common.Address]map[common.Hash]common.Hash
	StateOverrides   map[string]any
	Dependencies     []CrossRollupDependency
	OutboundMessages []CrossRollupMessage
}
