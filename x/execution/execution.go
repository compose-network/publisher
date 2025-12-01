// Package execution provides minimal interfaces for cross-chain coordinators to interact with execution layers.
//
// This package defines the adapter contract between the coordinator protocol and execution layer.
package execution

import (
	"context"

	pb "github.com/compose-network/publisher/proto/rollup/v1"
)

// Adapter is the minimal interface needed from the execution layer.
type Adapter interface {
	// SimulateTransactions executes transactions in a simulated environment
	// and returns the result including any cross-chain dependencies.
	SimulateTransactions(ctx context.Context, txs []TxData) (*SimulationResult, error)

	// InjectTransactions adds transactions to the execution layer's mempool.
	InjectTransactions(ctx context.Context, hashes []string) error

	// GetNonce returns the current nonce for an address.
	// Used for nonce gap policy enforcement.
	GetNonce(ctx context.Context, address []byte) (uint64, error)

	// GetTransaction retrieves transaction data by hash.
	// Used to convert hashes back to full transaction data.
	GetTransaction(hash string) (TxData, error)

	// SignPutInboxTransaction signs a putInbox transaction with the coordinator key.
	// Returns the signed transaction data and its hash.
	SignPutInboxTransaction(tx TxData) (signedTx TxData, hash string, err error)
}

// TxData represents transaction data in an execution-agnostic format.
// Implementations convert between this and execution-specific types (e.g., *types.Transaction).
type TxData struct {
	Hash      string
	From      []byte
	To        []byte
	Nonce     uint64
	Data      []byte
	Value     []byte
	Gas       uint64
	GasFeeCap []byte
	GasTipCap []byte
}

// SimulationResult holds the outcome of the transaction simulation.
type SimulationResult struct {
	Success      bool
	GasUsed      uint64
	Revert       []byte
	Dependencies []*pb.CIRCMessage
}
