package proofs

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
)

type MailboxInfoStruct struct {
	InboxChains  []common.Hash `json:"inbox_chains"`
	OutboxChains []common.Hash `json:"outbox_chains"`
	InboxRoots   []common.Hash `json:"inbox_roots"`
	OutboxRoots  []common.Hash `json:"outbox_roots"`
}

// OpSuccinctAggregationOutputs received from op-succinct
type OpSuccinctAggregationOutputs struct {
	L1Head           common.Hash       `json:"l1Head"`
	L2PreRoot        common.Hash       `json:"l2PreRoot"`
	L2PostRoot       common.Hash       `json:"l2PostRoot"`
	L2BlockNumber    uint64            `json:"l2BlockNumber"`
	RollupConfigHash common.Hash       `json:"rollupConfigHash"`
	MailboxRoot      common.Hash       `json:"mailboxRoot"`
	MailboxInfo      MailboxInfoStruct `json:"mailbox_info"`
	MultiBlockVKey   common.Hash       `json:"multi_block_vkey"`
	ProverAddress    common.Hash       `json:"prover_address"`
}

// AggregationOutputs sent to superblock prover
type AggregationOutputs struct {
	L1Head           common.Hash       `json:"l1_head"`
	L2PreRoot        common.Hash       `json:"l2_pre_root"`
	L2PostRoot       common.Hash       `json:"l2_post_root"`
	L2BlockNumber    uint64            `json:"l2_block_number"`
	RollupConfigHash common.Hash       `json:"rollup_config_hash"`
	MailboxRoot      common.Hash       `json:"mailboxRoot"`
	MailboxInfo      MailboxInfoStruct `json:"mailbox_info"`
	MultiBlockVKey   common.Hash       `json:"multi_block_vkey"`
	ProverAddress    common.Hash       `json:"prover_address"`
}

// ToAggregationOutputs converts op-succinct format to internal format
func (o OpSuccinctAggregationOutputs) ToAggregationOutputs() AggregationOutputs {
	return AggregationOutputs(o)
}

// AggregationOutputsWithChainID associates per-rollup outputs with a chain identifier.
type AggregationOutputsWithChainID struct {
	ChainID            uint32          `json:"chain_id"`
	AggregationOutputs json.RawMessage `json:"aggregation_outputs"`
}

// MailboxInfo represents mailbox state for a rollup chain.
type MailboxInfo struct {
	ChainID    uint32           `json:"chain_id"`
	InboxRoot  PublicValueBytes `json:"inbox_root"`  // bytes32
	OutboxRoot PublicValueBytes `json:"outbox_root"` // bytes32
}

// AggregationProofData packages per-rollup proof inputs.
// Must match the Rust AggregationProofData struct in superblock-prover.
type AggregationProofData struct {
	ChainID            uint32             `json:"chain_id"`
	AggregationOutputs AggregationOutputs `json:"aggregation_outputs"`
	CompressedProof    PublicValueBytes   `json:"compressed_proof"`
}
