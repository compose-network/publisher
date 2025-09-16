package proofs

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Status summarizes collection progress and prover job state for a superblock.
type Status struct {
	SuperblockHash   common.Hash          `json:"superblock_hash"`
	SuperblockNumber uint64               `json:"superblock_number"`
	Required         []uint32             `json:"required_chain_ids"`
	Received         map[uint32]time.Time `json:"received"`
	State            string               `json:"state"` // collecting|dispatched|proving|complete|failed
	JobID            string               `json:"job_id"`
	Error            string               `json:"error,omitempty"`
}

const (
	StateCollecting = "collecting"
	StateDispatched = "dispatched"
	StateProving    = "proving"
	StateComplete   = "complete"
	StateFailed     = "failed"
)
