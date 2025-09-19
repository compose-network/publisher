package proofs

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
)

// OpSuccinctAggregationOutputs represents op-succinct's camelCase format
type OpSuccinctAggregationOutputs struct {
	L1Head           common.Hash `json:"l1Head"`
	L2PreRoot        common.Hash `json:"l2PreRoot"`
	L2PostRoot       common.Hash `json:"l2PostRoot"`
	L2BlockNumber    uint64      `json:"l2BlockNumber"`
	RollupConfigHash common.Hash `json:"rollupConfigHash"`
}

// AggregationOutputs represents internal snake_case format for prover
type AggregationOutputs struct {
	L1Head           common.Hash `json:"l1_head"`
	L2PreRoot        common.Hash `json:"l2_pre_root"`
	L2PostRoot       common.Hash `json:"l2_post_root"`
	L2BlockNumber    uint64      `json:"l2_block_number"`
	RollupConfigHash common.Hash `json:"rollup_config_hash"`
}

// ToAggregationOutputs converts op-succinct format to internal format
func (o OpSuccinctAggregationOutputs) ToAggregationOutputs() AggregationOutputs {
	return AggregationOutputs(o)
}

// ABIEncode encodes AggregationOutputs into the 7*32 byte form expected by the prover.
func (a AggregationOutputs) ABIEncode() []byte {
	buf := make([]byte, 0, 7*32)
	buf = append(buf, a.L1Head.Bytes()...)
	buf = append(buf, a.L2PreRoot.Bytes()...)
	buf = append(buf, a.L2PostRoot.Bytes()...)
	var number [32]byte
	bn := a.L2BlockNumber
	for i := 0; i < 8; i++ {
		number[31-i] = byte(bn)
		bn >>= 8
	}
	buf = append(buf, number[:]...)
	buf = append(buf, a.RollupConfigHash.Bytes()...)
	return buf
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
	AggregationOutputs AggregationOutputs `json:"aggregation_outputs"`
	RawPublicValues    PublicValueBytes   `json:"raw_public_values"`
	CompressedProof    PublicValueBytes   `json:"compressed_proof"`
	AggVKey            [8]uint32          `json:"agg_vkey"`     // [u32; 8] in Rust
	MailboxInfo        []MailboxInfo      `json:"mailbox_info"` // Vec<MailboxInfo> in Rust
}
