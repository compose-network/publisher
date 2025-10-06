package types

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// BatchState represents the lifecycle state of a batch
type BatchState string

// Batch state constants define the possible states in the batch lifecycle
const (
	// StateCollecting means the batch is actively collecting L2 blocks
	StateCollecting BatchState = "collecting"

	// StateProving means the batch has been finalized and proofs are being generated
	StateProving BatchState = "proving"

	// StateCompleted means all proofs are generated and batch is ready for settlement
	StateCompleted BatchState = "completed"

	// StateFailed means the batch processing failed and requires intervention
	StateFailed BatchState = "failed"
)

// String returns the string representation of BatchState
func (s BatchState) String() string {
	return string(s)
}

// IsTerminal returns true if the state is terminal (completed or failed)
func (s BatchState) IsTerminal() bool {
	return s == StateCompleted || s == StateFailed
}

// IsActive returns true if the batch is actively processing
func (s BatchState) IsActive() bool {
	return s == StateCollecting || s == StateProving
}

// BatchInfo holds comprehensive information about a batch
type BatchInfo struct {
	// Identity
	ID      uint64     `json:"id"`
	ChainID uint32     `json:"chain_id"`
	State   BatchState `json:"state"`

	// Time boundaries
	StartEpoch uint64     `json:"start_epoch"`
	StartTime  time.Time  `json:"start_time"`
	EndTime    *time.Time `json:"end_time,omitempty"`

	// Slot boundaries
	StartSlot uint64  `json:"start_slot"`
	EndSlot   *uint64 `json:"end_slot,omitempty"`
	SlotCount uint64  `json:"slot_count"`

	// Block data
	Blocks []BatchBlockInfo `json:"blocks"`

	// Proof tracking
	ProofJobID *string `json:"proof_job_id,omitempty"`

	// Error information
	ErrorMessage *string `json:"error_message,omitempty"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BatchBlockInfo represents a single L2 block within a batch
type BatchBlockInfo struct {
	SlotNumber   uint64      `json:"slot_number"`
	BlockNumber  uint64      `json:"block_number"`
	BlockHash    common.Hash `json:"block_hash"`
	Timestamp    time.Time   `json:"timestamp"`
	TxCount      int         `json:"tx_count"`
	IncludedXTxs []string    `json:"included_xtxs,omitempty"` // Cross-chain transaction IDs
}

// BatchEvent represents an event in the batch lifecycle
type BatchEvent struct {
	Type      string      `json:"type"`
	BatchID   uint64      `json:"batch_id"`
	Data      interface{} `json:"data,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// BatchTrigger represents a signal to start a new batch
// This is sent by the EpochTracker when epoch % BatchFactor == 0
type BatchTrigger struct {
	TriggerEpoch uint64    `json:"trigger_epoch"`
	TriggerSlot  uint64    `json:"trigger_slot"`
	TriggerTime  time.Time `json:"trigger_time"`
}
