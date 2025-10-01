package http

import (
	"encoding/json"

	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs"
)

// submitReq is the JSON schema for POST routeSubmitOpSuccinct
type submitReq struct {
	SuperblockNumber uint64                              `json:"superblock_number"`
	SuperblockHash   string                              `json:"superblock_hash"` // 0x-hex
	ChainID          uint32                              `json:"chain_id"`
	L1Head           string                              `json:"l1_head"` // 0x-hex
	Aggregation      proofs.OpSuccinctAggregationOutputs `json:"aggregation_outputs"`
	L2StartBlock     uint64                              `json:"l2_start_block"`
	MailboxInfo      proofs.MailboxInfoStruct            `json:"mailbox_info"`
	AggVK            json.RawMessage                     `json:"agg_vk"`
	Proof            proofs.ProofBytes                   `json:"proof,omitempty"`
}
