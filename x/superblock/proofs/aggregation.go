package proofs

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
)

// AggregationOutputs mirrors op-succinct AggregationOutputs serialization.
// Supports both camelCase (op-succinct) and snake_case (internal) JSON formats.
type AggregationOutputs struct {
	L1Head           common.Hash    `json:"l1_head"`
	L2PreRoot        common.Hash    `json:"l2_pre_root"`
	L2PostRoot       common.Hash    `json:"l2_post_root"`
	L2BlockNumber    uint64         `json:"l2_block_number"`
	RollupConfigHash common.Hash    `json:"rollup_config_hash"`
	MultiBlockVKey   common.Hash    `json:"multi_block_vkey"`
	ProverAddress    common.Address `json:"prover_address"`
}

// UnmarshalJSON implements custom JSON unmarshaling to support both camelCase and snake_case field names.
func (a *AggregationOutputs) UnmarshalJSON(data []byte) error {
	// First try snake_case (current format)
	type snakeCase AggregationOutputs
	if err := json.Unmarshal(data, (*snakeCase)(a)); err == nil {
		return nil
	}

	// If that fails, try camelCase (op-succinct format)
	type camelCase struct {
		L1Head           common.Hash    `json:"l1Head"`
		L2PreRoot        common.Hash    `json:"l2PreRoot"`
		L2PostRoot       common.Hash    `json:"l2PostRoot"`
		L2BlockNumber    uint64         `json:"l2BlockNumber"`
		RollupConfigHash common.Hash    `json:"rollupConfigHash"`
		MultiBlockVKey   common.Hash    `json:"multiBlockVKey"`
		ProverAddress    common.Address `json:"proverAddress"`
	}

	var cc camelCase
	if err := json.Unmarshal(data, &cc); err != nil {
		return err
	}

	// Convert from camelCase to our struct
	a.L1Head = cc.L1Head
	a.L2PreRoot = cc.L2PreRoot
	a.L2PostRoot = cc.L2PostRoot
	a.L2BlockNumber = cc.L2BlockNumber
	a.RollupConfigHash = cc.RollupConfigHash
	a.MultiBlockVKey = cc.MultiBlockVKey
	a.ProverAddress = cc.ProverAddress

	return nil
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
	var addr [32]byte
	addrBytes := a.ProverAddress.Bytes()
	copy(addr[32-len(addrBytes):], addrBytes)
	buf = append(buf, addr[:]...)
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
