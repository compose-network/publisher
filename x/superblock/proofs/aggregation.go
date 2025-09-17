package proofs

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
)

// OpSuccinctAggregationOutputs represents op-succinct's camelCase format
type OpSuccinctAggregationOutputs struct {
	L1Head           common.Hash    `json:"l1Head"`
	L2PreRoot        common.Hash    `json:"l2PreRoot"`
	L2PostRoot       common.Hash    `json:"l2PostRoot"`
	L2BlockNumber    uint64         `json:"l2BlockNumber"`
	RollupConfigHash common.Hash    `json:"rollupConfigHash"`
	MultiBlockVKey   common.Hash    `json:"multiBlockVKey"`
	ProverAddress    common.Address `json:"proverAddress"`
}

// AggregationOutputs represents internal snake_case format for prover
type AggregationOutputs struct {
	L1Head           common.Hash `json:"l1_head"`
	L2PreRoot        common.Hash `json:"l2_pre_root"`
	L2PostRoot       common.Hash `json:"l2_post_root"`
	L2BlockNumber    uint64      `json:"l2_block_number"`
	RollupConfigHash common.Hash `json:"rollup_config_hash"`
	MultiBlockVKey   common.Hash `json:"multi_block_vkey"`
	ProverAddress    common.Hash `json:"prover_address"` // 32-byte padded address for prover
}

// ToAggregationOutputs converts op-succinct format to internal format
func (o OpSuccinctAggregationOutputs) ToAggregationOutputs() AggregationOutputs {
	// Pad the 20-byte address to 32 bytes for the prover
	var paddedAddress common.Hash
	copy(paddedAddress[12:], o.ProverAddress[:]) // Put address in last 20 bytes

	return AggregationOutputs{
		L1Head:           o.L1Head,
		L2PreRoot:        o.L2PreRoot,
		L2PostRoot:       o.L2PostRoot,
		L2BlockNumber:    o.L2BlockNumber,
		RollupConfigHash: o.RollupConfigHash,
		MultiBlockVKey:   o.MultiBlockVKey,
		ProverAddress:    paddedAddress,
	}
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
	buf = append(buf, a.MultiBlockVKey.Bytes()...)
	buf = append(buf, a.ProverAddress.Bytes()...) // ProverAddress is already 32 bytes
	return buf
}

// AggregationOutputsWithChainID associates per-rollup outputs with a chain identifier.
type AggregationOutputsWithChainID struct {
	ChainID            uint32          `json:"chain_id"`
	AggregationOutputs json.RawMessage `json:"aggregation_outputs"`
}

// AggregationProofData packages per-rollup proof inputs.
type AggregationProofData struct {
	AggregationOutputs AggregationOutputs `json:"aggregation_outputs"`
	RawPublicValues    []byte             `json:"raw_public_values"`
	CompressedProof    []byte             `json:"compressed_proof"`
	ChainID            []byte             `json:"chain_id"`
	SuperblockNumber   uint64             `json:"superblock_number"`
	VKey               string             `json:"vkey"`
}
