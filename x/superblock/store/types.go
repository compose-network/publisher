package store

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type Superblock struct {
	Number            uint64           `json:"number"`
	Slot              uint64           `json:"slot"`
	ParentHash        common.Hash      `json:"parent_hash"`
	Hash              common.Hash      `json:"hash"`
	MerkleRoot        common.Hash      `json:"merkle_root"`
	Timestamp         time.Time        `json:"timestamp"`
	IncludedXTs       []common.Hash    `json:"included_xts"`
	Proof             []byte           `json:"proof,omitempty"`
	L1TransactionHash common.Hash      `json:"l1_transaction_hash,omitempty"`
	Status            SuperblockStatus `json:"status"`
}

type SuperblockStatus string

const (
	SuperblockStatusPending    SuperblockStatus = "pending"
	SuperblockStatusSubmitted  SuperblockStatus = "submitted"
	SuperblockStatusConfirmed  SuperblockStatus = "confirmed"
	SuperblockStatusFinalized  SuperblockStatus = "finalized"
	SuperblockStatusRolledBack SuperblockStatus = "rolled_back"
)
