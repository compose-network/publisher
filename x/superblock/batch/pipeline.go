package batch

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs/collector"
)

// PipelineStage represents the current stage of proof pipeline processing
type PipelineStage string

const (
	StageIdle        PipelineStage = "idle"
	StageRangeProof  PipelineStage = "range_proof" // op-succinct range program
	StageAggregation PipelineStage = "aggregation" // op-succinct aggregation program
	StageNetworkAgg  PipelineStage = "network_agg" // superblock-prover network aggregation
	StageCompleted   PipelineStage = "completed"
	StageFailed      PipelineStage = "failed"
)

// PipelineJob represents a batch processing job in the proof pipeline
type PipelineJob struct {
	ID        string        `json:"id"`
	BatchID   uint64        `json:"batch_id"`
	ChainID   uint32        `json:"chain_id"`
	Stage     PipelineStage `json:"stage"`
	BatchInfo *BatchInfo    `json:"batch_info"`

	// Stage-specific job IDs
	RangeProofJobID *string `json:"range_proof_job_id,omitempty"`
	AggJobID        *string `json:"agg_job_id,omitempty"`
	NetworkAggJobID *string `json:"network_agg_job_id,omitempty"`

	// Results
	RangeProof *proofs.ProofBytes `json:"range_proof,omitempty"`
	AggProof   *proofs.ProofBytes `json:"agg_proof,omitempty"`
	FinalProof *proofs.ProofBytes `json:"final_proof,omitempty"`

	// Metadata
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	ErrorMessage *string   `json:"error_message,omitempty"`
	RetryCount   int       `json:"retry_count"`
}

// Pipeline orchestrates the proof generation flow: Range → Aggregation → Network Aggregation
type Pipeline struct {
	mu             sync.RWMutex
	log            zerolog.Logger
	batchManager   *Manager
	proofCollector collector.Service
	proverClient   proofs.ProverClient

	// Job tracking
	activeJobs map[string]*PipelineJob
	jobCounter uint64

	// Configuration
	maxConcurrentJobs int
	jobTimeout        time.Duration
	maxRetries        int
	retryDelay        time.Duration

	// Event channels
	jobEventCh      chan PipelineJobEvent
	completedJobsCh chan *PipelineJob

	// Control
	ctx      context.Context
	cancel   context.CancelFunc
	workerWg sync.WaitGroup
}

// PipelineJobEvent represents events during pipeline job processing
type PipelineJobEvent struct {
	Type      string        `json:"type"`
	JobID     string        `json:"job_id"`
	BatchID   uint64        `json:"batch_id"`
	Stage     PipelineStage `json:"stage"`
	Data      interface{}   `json:"data,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}

// PipelineConfig holds configuration for the proof pipeline
type PipelineConfig struct {
	MaxConcurrentJobs int           `mapstructure:"max_concurrent_jobs" yaml:"max_concurrent_jobs"`
	JobTimeout        time.Duration `mapstructure:"job_timeout"         yaml:"job_timeout"`
	MaxRetries        int           `mapstructure:"max_retries"         yaml:"max_retries"`
	RetryDelay        time.Duration `mapstructure:"retry_delay"         yaml:"retry_delay"`
}

// DefaultPipelineConfig returns sensible defaults
func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{
		MaxConcurrentJobs: 5,
		JobTimeout:        30 * time.Minute,
		MaxRetries:        3,
		RetryDelay:        5 * time.Minute,
	}
}

// NewPipeline creates a new proof pipeline orchestrator
func NewPipeline(
	cfg PipelineConfig,
	batchMgr *Manager,
	collector collector.Service,
	proverClient proofs.ProverClient,
	log zerolog.Logger,
) (*Pipeline, error) {
	if batchMgr == nil {
		return nil, fmt.Errorf("batch manager is required")
	}
	if collector == nil {
		return nil, fmt.Errorf("proof collector is required")
	}
	if proverClient == nil {
		return nil, fmt.Errorf("prover client is required")
	}

	logger := log.With().Str("component", "proof-pipeline").Logger()
	ctx, cancel := context.WithCancel(context.Background())

	p := &Pipeline{
		log:               logger,
		batchManager:      batchMgr,
		proofCollector:    collector,
		proverClient:      proverClient,
		activeJobs:        make(map[string]*PipelineJob),
		maxConcurrentJobs: cfg.MaxConcurrentJobs,
		jobTimeout:        cfg.JobTimeout,
		maxRetries:        cfg.MaxRetries,
		retryDelay:        cfg.RetryDelay,
		jobEventCh:        make(chan PipelineJobEvent, 100),
		completedJobsCh:   make(chan *PipelineJob, 50),
		ctx:               ctx,
		cancel:            cancel,
	}

	// Set defaults
	if p.maxConcurrentJobs <= 0 {
		p.maxConcurrentJobs = 5
	}
	if p.jobTimeout <= 0 {
		p.jobTimeout = 30 * time.Minute
	}
	if p.maxRetries <= 0 {
		p.maxRetries = 3
	}
	if p.retryDelay <= 0 {
		p.retryDelay = 5 * time.Minute
	}

	logger.Info().
		Int("max_concurrent_jobs", p.maxConcurrentJobs).
		Dur("job_timeout", p.jobTimeout).
		Int("max_retries", p.maxRetries).
		Dur("retry_delay", p.retryDelay).
		Msg("Proof pipeline initialized")

	return p, nil
}

// Start begins the pipeline processing
func (p *Pipeline) Start(ctx context.Context) error {
	p.log.Info().Msg("Starting proof pipeline")

	// Start event processing goroutine
	go p.eventLoop(ctx)

	// Start job processing workers
	for i := 0; i < p.maxConcurrentJobs; i++ {
		p.workerWg.Add(1)
		go p.jobWorker(ctx, i)
	}

	// Start batch monitor
	go p.batchMonitor(ctx)

	return nil
}

// Stop gracefully stops the pipeline
func (p *Pipeline) Stop(ctx context.Context) error {
	p.log.Info().Msg("Stopping proof pipeline")

	p.cancel()
	p.workerWg.Wait()

	close(p.jobEventCh)
	close(p.completedJobsCh)

	return nil
}

// batchMonitor watches for batches ready for proving
func (p *Pipeline) batchMonitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	p.log.Info().Msg("Batch monitor started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.checkForNewBatches()
		}
	}
}

// checkForNewBatches looks for batches ready for proving and creates pipeline jobs
func (p *Pipeline) checkForNewBatches() {
	readyBatches := p.batchManager.GetBatchesReadyForProving()

	for _, batch := range readyBatches {
		if err := p.createPipelineJob(batch); err != nil {
			p.log.Error().
				Err(err).
				Uint64("batch_id", batch.ID).
				Msg("Failed to create pipeline job for batch")
		}
	}
}

// createPipelineJob creates a new pipeline job for a batch
func (p *Pipeline) createPipelineJob(batch *BatchInfo) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if job already exists for this batch
	for _, job := range p.activeJobs {
		if job.BatchID == batch.ID {
			p.log.Debug().Uint64("batch_id", batch.ID).Msg("Pipeline job already exists for batch")
			return nil
		}
	}

	p.jobCounter++
	jobID := fmt.Sprintf("pipeline-%d-%d", batch.ID, p.jobCounter)

	job := &PipelineJob{
		ID:        jobID,
		BatchID:   batch.ID,
		ChainID:   batch.ChainID,
		Stage:     StageRangeProof,
		BatchInfo: batch,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	p.activeJobs[jobID] = job

	// Update batch with job reference
	if err := p.batchManager.UpdateBatchProofJob(batch.ID, jobID); err != nil {
		p.log.Error().Err(err).Msg("Failed to update batch with proof job ID")
	}

	p.log.Info().
		Str("job_id", jobID).
		Uint64("batch_id", batch.ID).
		Uint32("chain_id", batch.ChainID).
		Int("block_count", len(batch.Blocks)).
		Msg("Created pipeline job for batch")

	// Emit event
	p.emitJobEvent("job_created", job, nil)

	return nil
}

// eventLoop processes pipeline events
func (p *Pipeline) eventLoop(ctx context.Context) {
	p.log.Info().Msg("Pipeline event loop started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.ctx.Done():
			return
		case completedJob := <-p.completedJobsCh:
			p.handleCompletedJob(completedJob)
		}
	}
}

// jobWorker processes pipeline jobs
func (p *Pipeline) jobWorker(ctx context.Context, workerID int) {
	defer p.workerWg.Done()

	logger := p.log.With().Int("worker_id", workerID).Logger()
	logger.Info().Msg("Pipeline worker started")

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("Pipeline worker stopping")
			return
		case <-p.ctx.Done():
			logger.Info().Msg("Pipeline worker canceled")
			return
		case <-ticker.C:
			if job := p.getNextJob(); job != nil {
				logger.Info().
					Str("job_id", job.ID).
					Uint64("batch_id", job.BatchID).
					Str("stage", string(job.Stage)).
					Msg("Processing pipeline job")

				if err := p.processJob(ctx, job); err != nil {
					logger.Error().
						Err(err).
						Str("job_id", job.ID).
						Msg("Pipeline job failed")

					p.handleJobError(job, err)
				}
			}
		}
	}
}

// getNextJob returns the next job to process
func (p *Pipeline) getNextJob() *PipelineJob {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Find the oldest job that's not in a terminal state
	var oldestJob *PipelineJob
	var oldestTime time.Time

	for _, job := range p.activeJobs {
		if job.Stage != StageCompleted && job.Stage != StageFailed {
			if oldestJob == nil || job.CreatedAt.Before(oldestTime) {
				oldestJob = job
				oldestTime = job.CreatedAt
			}
		}
	}

	return oldestJob
}

// processJob processes a single pipeline job through its current stage
func (p *Pipeline) processJob(ctx context.Context, job *PipelineJob) error {
	switch job.Stage {
	case StageRangeProof:
		return p.processRangeProofStage(job)
	case StageAggregation:
		return p.processAggregationStage(job)
	case StageNetworkAgg:
		return p.processNetworkAggStage(ctx, job)
	case StageIdle:
		return fmt.Errorf("job in idle stage should not be processed")
	case StageCompleted:
		return fmt.Errorf("job already completed")
	case StageFailed:
		return fmt.Errorf("job already failed")
	default:
		return fmt.Errorf("unknown pipeline stage: %s", job.Stage)
	}
}

// processRangeProofStage handles the op-succinct range proof generation
func (p *Pipeline) processRangeProofStage(job *PipelineJob) error {
	p.log.Info().
		Str("job_id", job.ID).
		Uint64("batch_id", job.BatchID).
		Msg("Processing range proof stage")

	// For now, simulate range proof generation since we need op-succinct integration
	// In production, this would call the actual op-succinct range program

	// Simulate processing time
	time.Sleep(2 * time.Second)

	// Create mock range proof
	rangeProof := proofs.ProofBytes("mock_range_proof_" + job.ID)
	job.RangeProof = &rangeProof
	job.Stage = StageAggregation
	job.UpdatedAt = time.Now()

	p.log.Info().
		Str("job_id", job.ID).
		Msg("Range proof stage completed")

	p.emitJobEvent("stage_completed", job, map[string]interface{}{
		"completed_stage": StageRangeProof,
		"next_stage":      StageAggregation,
	})

	return nil
}

// processAggregationStage handles the op-succinct aggregation
func (p *Pipeline) processAggregationStage(job *PipelineJob) error {
	p.log.Info().
		Str("job_id", job.ID).
		Uint64("batch_id", job.BatchID).
		Msg("Processing aggregation stage")

	// Simulate aggregation processing
	time.Sleep(2 * time.Second)

	// Create mock aggregation proof
	aggProof := proofs.ProofBytes("mock_agg_proof_" + job.ID)
	job.AggProof = &aggProof
	job.Stage = StageNetworkAgg
	job.UpdatedAt = time.Now()

	p.log.Info().
		Str("job_id", job.ID).
		Msg("Aggregation stage completed")

	p.emitJobEvent("stage_completed", job, map[string]interface{}{
		"completed_stage": StageAggregation,
		"next_stage":      StageNetworkAgg,
	})

	return nil
}

// processNetworkAggStage handles the superblock-prover network aggregation
func (p *Pipeline) processNetworkAggStage(ctx context.Context, job *PipelineJob) error {
	p.log.Info().
		Str("job_id", job.ID).
		Uint64("batch_id", job.BatchID).
		Msg("Processing network aggregation stage")

	// Create proper prover input with batch data
	aggData := proofs.AggregationProofData{
		ChainID:         job.BatchInfo.ChainID,
		CompressedProof: proofs.PublicValueBytes(*job.AggProof),
		AggregationOutputs: proofs.AggregationOutputs{
			L2BlockNumber: job.BatchInfo.Blocks[len(job.BatchInfo.Blocks)-1].BlockNumber,
			// TODO: Set proper values from actual proof data
			// L1Head, L2PreRoot, L2PostRoot, RollupConfigHash, MailboxRoot, MultiBlockVKey, ProverAddress
		},
		AggVKey: [8]int{1, 2, 3, 4, 5, 6, 7, 8}, // TODO: Set proper vkey from proof
		MailboxInfo: proofs.MailboxInfoStruct{
			InboxChains:  []common.Hash{},
			OutboxChains: []common.Hash{},
			InboxRoots:   []common.Hash{},
			OutboxRoots:  []common.Hash{},
		},
	}

	proofInput := proofs.ProofJobInput{
		ProofType: "superblock_aggregation",
		Input: proofs.SuperblockProverInput{
			PreviousBatch: proofs.SuperblockBatch{
				SuperblockNumber: job.BatchInfo.ID - 1,
			},
			NewBatch: proofs.SuperblockBatch{
				SuperblockNumber: job.BatchInfo.ID,
			},
			AggregationProofs: []proofs.AggregationProofData{aggData},
		},
	}

	jobID, err := p.proverClient.RequestProof(ctx, proofInput)
	if err != nil {
		return fmt.Errorf("request network aggregation proof: %w", err)
	}

	job.NetworkAggJobID = &jobID
	job.UpdatedAt = time.Now()

	// Poll for completion
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
			status, err := p.proverClient.GetStatus(ctx, jobID)
			if err != nil {
				p.log.Error().Err(err).Str("job_id", jobID).Msg("Failed to get proof status")
				continue
			}

			switch status.Status {
			case "completed":
				proof := proofs.ProofBytes(status.Proof)
				job.FinalProof = &proof
				job.Stage = StageCompleted
				job.UpdatedAt = time.Now()

				p.log.Info().
					Str("job_id", job.ID).
					Str("prover_job_id", jobID).
					Msg("Network aggregation stage completed")

				// Send completed job
				select {
				case p.completedJobsCh <- job:
				default:
					p.log.Warn().Msg("Completed jobs channel full")
				}

				p.emitJobEvent("stage_completed", job, map[string]interface{}{
					"completed_stage": StageNetworkAgg,
					"final_stage":     true,
				})

				return nil
			case "failed":
				return fmt.Errorf("network aggregation proof failed")
			}

			p.log.Debug().
				Str("prover_job_id", jobID).
				Str("status", status.Status).
				Msg("Waiting for network aggregation proof")
		}
	}
}

// handleJobError handles errors during job processing
func (p *Pipeline) handleJobError(job *PipelineJob, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	job.RetryCount++
	errMsg := err.Error()
	job.ErrorMessage = &errMsg
	job.UpdatedAt = time.Now()

	if job.RetryCount >= p.maxRetries {
		job.Stage = StageFailed
		p.log.Error().
			Str("job_id", job.ID).
			Uint64("batch_id", job.BatchID).
			Int("retry_count", job.RetryCount).
			Str("error", errMsg).
			Msg("Pipeline job failed after max retries")

		// Mark batch as failed
		if err := p.batchManager.MarkBatchFailed(job.BatchID, errMsg); err != nil {
			p.log.Error().Err(err).Msg("Failed to mark batch as failed")
		}

		p.emitJobEvent("job_failed", job, map[string]interface{}{
			"error":       errMsg,
			"retry_count": job.RetryCount,
		})
	} else {
		p.log.Warn().
			Str("job_id", job.ID).
			Int("retry_count", job.RetryCount).
			Int("max_retries", p.maxRetries).
			Str("error", errMsg).
			Msg("Pipeline job will retry")

		p.emitJobEvent("job_retry", job, map[string]interface{}{
			"error":       errMsg,
			"retry_count": job.RetryCount,
		})
	}
}

// handleCompletedJob handles completed pipeline jobs
func (p *Pipeline) handleCompletedJob(job *PipelineJob) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.log.Info().
		Str("job_id", job.ID).
		Uint64("batch_id", job.BatchID).
		Msg("Pipeline job completed successfully")

	// Mark batch as completed
	if err := p.batchManager.MarkBatchCompleted(job.BatchID); err != nil {
		p.log.Error().Err(err).Msg("Failed to mark batch as completed")
	}

	// Submit to shared publisher (this would be implemented based on your SP integration)
	p.submitToSharedPublisher(job)

	p.emitJobEvent("job_completed", job, map[string]interface{}{
		"final_proof_size": len(*job.FinalProof),
	})

	// Clean up completed job
	delete(p.activeJobs, job.ID)
}

// submitToSharedPublisher submits the completed proof to the shared publisher
func (p *Pipeline) submitToSharedPublisher(job *PipelineJob) {
	p.log.Info().
		Str("job_id", job.ID).
		Uint64("batch_id", job.BatchID).
		Int("proof_size", len(*job.FinalProof)).
		Msg("Submitting batch proof to shared publisher")

	// Create submission for the proof collector
	submission := proofs.Submission{
		SuperblockNumber: job.BatchInfo.ID,
		SuperblockHash:   [32]byte{}, // Calculate proper hash
		ChainID:          job.ChainID,
		Proof:            []byte(*job.FinalProof),
		ReceivedAt:       time.Now(),
	}

	// Submit to proof collector
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := p.proofCollector.SubmitOpSuccinct(ctx, submission); err != nil {
		p.log.Error().Err(err).Str("job_id", job.ID).Msg("Failed to submit proof to collector")
		return
	}

	p.log.Info().Str("job_id", job.ID).Msg("Successfully submitted proof to collector")
}

// emitJobEvent emits a pipeline job event
func (p *Pipeline) emitJobEvent(eventType string, job *PipelineJob, data interface{}) {
	event := PipelineJobEvent{
		Type:      eventType,
		JobID:     job.ID,
		BatchID:   job.BatchID,
		Stage:     job.Stage,
		Data:      data,
		Timestamp: time.Now(),
	}

	select {
	case p.jobEventCh <- event:
	default:
		p.log.Warn().Str("event_type", eventType).Msg("Job event channel full, dropping event")
	}
}

// GetActiveJobs returns information about currently active jobs
func (p *Pipeline) GetActiveJobs() []*PipelineJob {
	p.mu.RLock()
	defer p.mu.RUnlock()

	jobs := make([]*PipelineJob, 0, len(p.activeJobs))
	for _, job := range p.activeJobs {
		jobCopy := *job
		jobs = append(jobs, &jobCopy)
	}

	return jobs
}

// GetJobEvents returns the channel for job events
func (p *Pipeline) GetJobEvents() <-chan PipelineJobEvent {
	return p.jobEventCh
}

// GetStats returns pipeline statistics
func (p *Pipeline) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := map[string]interface{}{
		"active_jobs":         len(p.activeJobs),
		"max_concurrent_jobs": p.maxConcurrentJobs,
		"job_timeout":         p.jobTimeout.String(),
		"max_retries":         p.maxRetries,
		"jobs_by_stage":       make(map[string]int),
	}

	stageCount := stats["jobs_by_stage"].(map[string]int)
	for _, job := range p.activeJobs {
		stageCount[string(job.Stage)]++
	}

	return stats
}
