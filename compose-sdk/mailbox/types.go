// Package mailbox provides parsing and building of mailbox contract operations.
package mailbox

import (
	"math/big"

	"github.com/compose-network/compose-sdk/protocol"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Type aliases for backward compatibility.
// These types are defined in protocol package as the single source of truth.
type (
	CrossRollupDependency = protocol.CrossRollupDependency
	CrossRollupMessage    = protocol.CrossRollupMessage
)

// CallFrame represents a call in the geth callTracer output.
type CallFrame struct {
	Type    string         `json:"type"`
	From    common.Address `json:"from"`
	To      common.Address `json:"to"`
	Value   *hexutil.Big   `json:"value,omitempty"`
	Gas     hexutil.Uint64 `json:"gas"`
	GasUsed hexutil.Uint64 `json:"gasUsed"`
	Input   hexutil.Bytes  `json:"input"`
	Output  hexutil.Bytes  `json:"output,omitempty"`
	Error   string         `json:"error,omitempty"`
	Calls   []CallFrame    `json:"calls,omitempty"`
}

// CallTraceResult holds the result of debug_traceCall with callTracer.
type CallTraceResult struct {
	CallFrame
}

// MailboxCall represents a parsed read() or write() call to the mailbox contract.
type MailboxCall struct {
	// For read() calls
	ChainMessageSender *big.Int       // chainMessageSender parameter
	Sender             common.Address // sender parameter (on source chain)

	// For write() calls
	ChainMessageRecipient *big.Int       // chainMessageRecipient parameter
	Receiver              common.Address // receiver parameter (on dest chain)

	// Common fields
	SessionID *big.Int
	Label     []byte
	Data      []byte

	// Call type flags
	IsRead  bool
	IsWrite bool

	// Derived fields
	ChainSrc  *big.Int // For write: this chain, for read: chainMessageSender
	ChainDest *big.Int // For write: chainMessageRecipient, for read: this chain
}

// SimulationState holds the complete result of analyzing a transaction trace.
type SimulationState struct {
	Success          bool
	Dependencies     []CrossRollupDependency
	OutboundMessages []CrossRollupMessage
	TxHash           common.Hash
	GasUsed          uint64
}

// RequiresCoordination returns true if the transaction involves cross-chain operations.
func (s *SimulationState) RequiresCoordination() bool {
	return len(s.Dependencies) > 0 || len(s.OutboundMessages) > 0
}
