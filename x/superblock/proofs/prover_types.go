package proofs

import "context"

// ProverClient defines the interface for interacting with the external superblock-prover.
type ProverClient interface {
	RequestProof(ctx context.Context, job ProofJobInput) (jobID string, err error)
	GetStatus(ctx context.Context, jobID string) (ProofJobStatus, error)
}

// ProofJobInput bundles the prover payload and selected proof type.
type ProofJobInput struct {
	ProofType string                `json:"proof_type"`
	Input     SuperblockProverInput `json:"input"`
}

// ProofJobStatus represents the prover's reported state.
type ProofJobStatus struct {
	Status        string `json:"status"`
	Proof         []byte `json:"proof,omitempty"`
	ProvingTimeMS *uint64
	Cycles        *uint64
}

// SuperblockProverInput mirrors the Rust prover input schema.
type SuperblockProverInput struct {
	Superblocks       []ProverSuperblock     `json:"superblocks"`
	AggregationProofs []AggregationProofData `json:"aggregation_proofs"`
}

// ProverSuperblock matches the prover's expected Superblock representation.
type ProverSuperblock struct {
	Number            uint64          `json:"number"`
	Slot              uint64          `json:"slot"`
	ParentHash        []byte          `json:"parent_hash"`
	Hash              []byte          `json:"hash"`
	MerkleRoot        []byte          `json:"merkle_root"`
	Timestamp         uint64          `json:"timestamp"`
	L2Blocks          []ProverL2Block `json:"l2_blocks"`
	IncludedXTs       [][]byte        `json:"included_xts"`
	L1TransactionHash []byte          `json:"l1_transaction_hash,omitempty"`
}

// ProverL2Block mirrors the prover-facing L2 block representation.
type ProverL2Block struct {
	Slot            uint64   `json:"slot"`
	ChainID         []byte   `json:"chain_id"`
	BlockNumber     uint64   `json:"block_number"`
	BlockHash       []byte   `json:"block_hash"`
	ParentBlockHash []byte   `json:"parent_block_hash"`
	IncludedXTs     [][]byte `json:"included_xts"`
	Block           []byte   `json:"block"`
}
