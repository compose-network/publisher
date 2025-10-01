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
	L1Head           common.Hash    `json:"l1Head"`
	L2PreRoot        common.Hash    `json:"l2PreRoot"`
	L2PostRoot       common.Hash    `json:"l2PostRoot"`
	L2BlockNumber    uint64         `json:"l2BlockNumber"`
	RollupConfigHash common.Hash    `json:"rollupConfigHash"`
	MailboxRoot      common.Hash    `json:"mailboxRoot"`
	MultiBlockVKey   common.Hash    `json:"multiBlockVKey"`
	ProverAddress    common.Address `json:"proverAddress"`
}

// AggregationOutputs sent to superblock prover
type AggregationOutputs struct {
	L1Head           common.Hash    `json:"l1_head"`
	L2PreRoot        common.Hash    `json:"l2_pre_root"`
	L2PostRoot       common.Hash    `json:"l2_post_root"`
	L2BlockNumber    uint64         `json:"l2_block_number"`
	RollupConfigHash common.Hash    `json:"rollup_config_hash"`
	MailboxRoot      common.Hash    `json:"mailbox_root"`
	MultiBlockVKey   common.Hash    `json:"multi_block_vkey"`
	ProverAddress    common.Address `json:"prover_address"`
}

// ToAggregationOutputs converts op-succinct format to internal format
func (o OpSuccinctAggregationOutputs) ToAggregationOutputs() AggregationOutputs {
	return AggregationOutputs(o)
}

// ABIEncode encodes AggregationOutputs into the 8*32 byte form expected by the prover.
//
// Encodes all fields:
// l1Head, l2PreRoot, l2PostRoot, l2BlockNumber, rollupConfigHash, mailboxRoot,
// multiBlockVKey, proverAddress
func (a AggregationOutputs) ABIEncode() []byte {
	buf := make([]byte, 0, 8*32)
	buf = append(buf, a.L1Head.Bytes()...)
	buf = append(buf, a.L2PreRoot.Bytes()...)
	buf = append(buf, a.L2PostRoot.Bytes()...)

	// Encode L2BlockNumber as big-endian in 32 bytes
	var number [32]byte
	bn := a.L2BlockNumber
	for i := 0; i < 8; i++ {
		number[31-i] = byte(bn)
		bn >>= 8
	}
	buf = append(buf, number[:]...)
	buf = append(buf, a.RollupConfigHash.Bytes()...)
	buf = append(buf, a.MailboxRoot.Bytes()...)
	buf = append(buf, a.MultiBlockVKey.Bytes()...)

	// Encode ProverAddress (20 bytes) left-padded to 32 bytes
	var address [32]byte
	copy(address[12:], a.ProverAddress.Bytes())
	buf = append(buf, address[:]...)

	return buf
}

// AggregationOutputsWithChainID associates per-rollup outputs with a chain identifier.
type AggregationOutputsWithChainID struct {
	ChainID            uint32          `json:"chain_id"`
	AggregationOutputs json.RawMessage `json:"aggregation_outputs"`
}

// AggregationProofData packages per-rollup proof inputs.
// Must match the Rust AggregationProofData struct in superblock-prover.
type AggregationProofData struct {
	ChainID            uint32             `json:"chain_id"`
	AggregationOutputs AggregationOutputs `json:"aggregation_outputs"`
	CompressedProof    PublicValueBytes   `json:"compressed_proof"`
	AggVKey            [8]int             `json:"agg_vkey"`
	MailboxInfo        MailboxInfoStruct  `json:"mailbox_info"`
}
