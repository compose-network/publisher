package types

import (
	"time"

	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs"
)

// PipelineStage represents the current stage of proof pipeline processing
type PipelineStage string

// Pipeline stage constants define the proof generation workflow
const (
	// StageIdle means the job is queued but not yet started
	StageIdle PipelineStage = "idle"

	// StageRangeProof means the range program is executing (op-succinct)
	// This validates a range of L2 blocks
	StageRangeProof PipelineStage = "range_proof"

	// StageAggregation means the aggregation program is executing (op-succinct)
	// This aggregates multiple range proofs into a single batch proof
	StageAggregation PipelineStage = "aggregation"

	// StageNetworkAgg means the network aggregation program is executing (superblock-prover)
	// This creates the final superblock proof combining all rollup proofs
	StageNetworkAgg PipelineStage = "network_agg"

	// StageCompleted means all proof stages completed successfully
	StageCompleted PipelineStage = "completed"

	// StageFailed means proof generation failed at some stage
	StageFailed PipelineStage = "failed"
)

// String returns the string representation of PipelineStage
func (s PipelineStage) String() string {
	return string(s)
}

// IsTerminal returns true if the stage is terminal (completed or failed)
func (s PipelineStage) IsTerminal() bool {
	return s == StageCompleted || s == StageFailed
}

// IsProcessing returns true if the pipeline is actively processing proofs
func (s PipelineStage) IsProcessing() bool {
	return s == StageRangeProof || s == StageAggregation || s == StageNetworkAgg
}

// PipelineJob represents a batch proof generation job in the pipeline
type PipelineJob struct {
	// Identity
	ID      string        `json:"id"`
	BatchID uint64        `json:"batch_id"`
	ChainID uint32        `json:"chain_id"`
	Stage   PipelineStage `json:"stage"`

	// Batch data
	BatchInfo *BatchInfo `json:"batch_info"`

	// Stage-specific job IDs (external prover system job IDs)
	RangeProofJobID *string `json:"range_proof_job_id,omitempty"`
	AggJobID        *string `json:"agg_job_id,omitempty"`
	NetworkAggJobID *string `json:"network_agg_job_id,omitempty"`

	// Proof results from each stage
	RangeProof *proofs.ProofBytes `json:"range_proof,omitempty"`
	AggProof   *proofs.ProofBytes `json:"agg_proof,omitempty"`
	FinalProof *proofs.ProofBytes `json:"final_proof,omitempty"`

	// Error tracking
	ErrorMessage *string `json:"error_message,omitempty"`
	RetryCount   int     `json:"retry_count"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PipelineJobEvent represents an event during pipeline job processing
type PipelineJobEvent struct {
	Type      string        `json:"type"`
	JobID     string        `json:"job_id"`
	BatchID   uint64        `json:"batch_id"`
	Stage     PipelineStage `json:"stage"`
	Data      interface{}   `json:"data,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}
