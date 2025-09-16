package superblock

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/rs/zerolog"

	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs"
	apicollector "github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs/collector"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/store"
)

type proofPipeline struct {
	cfg       ProofsConfig
	collector apicollector.Service
	prover    proofs.ProverClient
	sbStore   store.SuperblockStore
	log       zerolog.Logger
	pollEvery time.Duration

	publishFn func(context.Context, *store.Superblock, []byte) error

	mu   sync.Mutex
	jobs map[string]proofJob
	quit chan struct{}
	once sync.Once
}

type proofJob struct {
	hash      common.Hash
	number    uint64
	proofType string
}

func newProofPipeline(
	cfg ProofsConfig,
	collector apicollector.Service,
	prover proofs.ProverClient,
	sbStore store.SuperblockStore,
	publishFn func(context.Context, *store.Superblock, []byte) error,
	log zerolog.Logger,
) *proofPipeline {
	if !cfg.Enabled || collector == nil || prover == nil {
		return nil
	}
	poll := cfg.Prover.PollInterval
	if poll <= 0 {
		poll = 10 * time.Second
	}
	return &proofPipeline{
		cfg:       cfg,
		collector: collector,
		prover:    prover,
		sbStore:   sbStore,
		publishFn: publishFn,
		log:       log.With().Str("component", "proof-pipeline").Logger(),
		pollEvery: poll,
		jobs:      make(map[string]proofJob),
		quit:      make(chan struct{}),
	}
}

func (p *proofPipeline) Start(ctx context.Context) {
	if p == nil {
		return
	}

	p.log.Info().
		Str("proof_type", p.cfg.Prover.ProofType).
		Dur("poll_interval", p.pollEvery).
		Msg("Proof pipeline enabled")

	go p.pollLoop(ctx)
}

func (p *proofPipeline) Stop() {
	if p == nil {
		return
	}

	p.once.Do(func() { close(p.quit) })
}

func (p *proofPipeline) HandleSuperblock(ctx context.Context, sb *store.Superblock) error {
	if p == nil {
		return nil
	}
	subs, err := p.collector.ListSubmissions(ctx, sb.Hash)
	if err != nil {
		p.log.Debug().Err(err).Uint64("superblock", sb.Number).Msg("No submissions yet for superblock")
		return err
	}
	if len(subs) == 0 {
		p.log.Debug().Uint64("superblock", sb.Number).Msg("Skipping proof request: no submissions present")
		return nil
	}

	required := p.requiredChainIDs(subs)
	ready := p.isReady(required, subs)
	if !ready {
		missing := p.missingChains(required, subs)
		p.log.Info().
			Uint64("superblock", sb.Number).
			Ints("missing_chains", missing).
			Int("received", len(subs)).
			Msg("Waiting for remaining chain proofs")
		_ = p.collector.UpdateStatus(ctx, sb.Hash, func(st *proofs.Status) {
			st.Required = required
			if st.State == "" {
				st.State = proofs.StateCollecting
			}
		})
		return nil
	}

	job := p.buildProofJobInput(ctx, sb, subs)

	jobID, err := p.prover.RequestProof(ctx, job)
	if err != nil {
		_ = p.collector.UpdateStatus(ctx, sb.Hash, func(st *proofs.Status) {
			st.State = proofs.StateFailed
			st.Error = err.Error()
		})
		return fmt.Errorf("request proof: %w", err)
	}

	if err := p.collector.UpdateStatus(ctx, sb.Hash, func(st *proofs.Status) {
		st.Required = required
		st.State = proofs.StateProving
		st.JobID = jobID
		st.Error = ""
	}); err != nil {
		p.log.Warn().Err(err).Uint64("superblock", sb.Number).Msg("Failed to update status post dispatch")
	}

	p.mu.Lock()
	p.jobs[jobID] = proofJob{hash: sb.Hash, number: sb.Number, proofType: job.ProofType}
	p.mu.Unlock()

	p.log.Info().Str("job_id", jobID).Uint64("superblock", sb.Number).Msg("Proof job dispatched")
	return nil
}

func (p *proofPipeline) requiredChainIDs(subs []proofs.Submission) []uint32 {
	if len(p.cfg.Collector.RequiredChainIDs) > 0 {
		return append([]uint32(nil), p.cfg.Collector.RequiredChainIDs...)
	}
	seen := make(map[uint32]struct{}, len(subs))
	for _, s := range subs {
		seen[s.ChainID] = struct{}{}
	}
	out := make([]uint32, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	return out
}

func (p *proofPipeline) isReady(required []uint32, subs []proofs.Submission) bool {
	if !p.cfg.Collector.RequireAllChains {
		return len(subs) > 0
	}
	have := make(map[uint32]struct{}, len(subs))
	for _, s := range subs {
		have[s.ChainID] = struct{}{}
	}
	for _, id := range required {
		if _, ok := have[id]; !ok {
			return false
		}
	}
	return true
}

func (p *proofPipeline) buildProofJobInput(
	ctx context.Context,
	sb *store.Superblock,
	subs []proofs.Submission,
) proofs.ProofJobInput {
	superblocks := p.collectSuperblocks(ctx, sb)

	aggProofs := make([]proofs.AggregationProofData, 0, len(subs))
	for _, s := range subs {
		raw := s.Aggregation.ABIEncode()
		proofBytes := make([]byte, len(s.Proof))
		copy(proofBytes, s.Proof)

		chainID := make([]byte, 4)
		binary.BigEndian.PutUint32(chainID, s.ChainID)

		aggProofs = append(aggProofs, proofs.AggregationProofData{
			AggregationOutputs: s.Aggregation,
			RawPublicValues:    raw,
			CompressedProof:    proofBytes,
			ChainID:            chainID,
			SuperblockNumber:   s.SuperblockNumber,
			VKey:               deriveVKeyString(s.AggVerifyingKey),
		})
	}

	return proofs.ProofJobInput{
		ProofType: p.cfg.Prover.ProofType,
		Input: proofs.SuperblockProverInput{
			Superblocks:       superblocks,
			AggregationProofs: aggProofs,
		},
	}
}

func (p *proofPipeline) collectSuperblocks(
	ctx context.Context,
	current *store.Superblock,
) []proofs.ProverSuperblock {
	result := []proofs.ProverSuperblock{convertSuperblock(current)}
	if current.Number > 0 {
		prev, err := p.sbStore.GetSuperblock(ctx, current.Number-1)
		if err == nil {
			result = append([]proofs.ProverSuperblock{convertSuperblock(prev)}, result...)
		}
	}
	return result
}

func convertSuperblock(sb *store.Superblock) proofs.ProverSuperblock {
	psb := proofs.ProverSuperblock{
		Number:     sb.Number,
		Slot:       sb.Slot,
		ParentHash: sb.ParentHash.Bytes(),
		Hash:       sb.Hash.Bytes(),
		MerkleRoot: sb.MerkleRoot.Bytes(),
		Timestamp:  uint64(sb.Timestamp.Unix()),
	}
	for _, blk := range sb.L2Blocks {
		psb.L2Blocks = append(psb.L2Blocks, proofs.ProverL2Block{
			Slot:            blk.GetSlot(),
			ChainID:         append([]byte(nil), blk.GetChainId()...),
			BlockNumber:     blk.GetBlockNumber(),
			BlockHash:       append([]byte(nil), blk.GetBlockHash()...),
			ParentBlockHash: append([]byte(nil), blk.GetParentBlockHash()...),
			IncludedXTs:     cloneSlices(blk.GetIncludedXts()),
			Block:           append([]byte(nil), blk.GetBlock()...),
		})
	}
	for _, xt := range sb.IncludedXTs {
		psb.IncludedXTs = append(psb.IncludedXTs, xt.Bytes())
	}
	if sb.L1TransactionHash != (common.Hash{}) {
		psb.L1TransactionHash = sb.L1TransactionHash.Bytes()
	}
	return psb
}

func cloneSlices(src [][]byte) [][]byte {
	out := make([][]byte, len(src))
	for i, b := range src {
		out[i] = append([]byte(nil), b...)
	}
	return out
}

func deriveVKeyString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return hexutil.Encode(raw)
}

func (p *proofPipeline) pollLoop(ctx context.Context) {
	if p == nil {
		return
	}
	ticker := time.NewTicker(p.pollEvery)
	defer ticker.Stop()
	statsTicker := time.NewTicker(5 * p.pollEvery)
	defer statsTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.quit:
			return
		case <-ticker.C:
			p.pollOnce(ctx)
		case <-statsTicker.C:
			p.logStats()
		}
	}
}

func (p *proofPipeline) pollOnce(ctx context.Context) {
	p.mu.Lock()
	jobs := make(map[string]proofJob, len(p.jobs))
	for id, job := range p.jobs {
		jobs[id] = job
	}
	p.mu.Unlock()

	for id, job := range jobs {
		status, err := p.prover.GetStatus(ctx, id)
		if err != nil {
			p.log.Warn().Err(err).Str("job_id", id).Msg("Failed to fetch proof status")
			continue
		}
		switch strings.ToLower(status.Status) {
		case "pending", "running", "proving":
			continue
		case "failed":
			_ = p.collector.UpdateStatus(ctx, job.hash, func(st *proofs.Status) {
				st.State = proofs.StateFailed
				st.Error = "prover reported failure"
			})
			p.removeJob(id)
		case "completed":
			p.handleCompleted(ctx, id, job, status)
		default:
			p.log.Warn().Str("job_id", id).Str("status", status.Status).Msg("Unknown proof job status")
		}
	}
}

func (p *proofPipeline) handleCompleted(ctx context.Context, jobID string, job proofJob, status proofs.ProofJobStatus) {
	proofBytes := status.Proof
	if len(proofBytes) == 0 {
		p.log.Warn().Str("job_id", jobID).Msg("Completed proof job returned empty proof")
		_ = p.collector.UpdateStatus(ctx, job.hash, func(st *proofs.Status) {
			st.State = proofs.StateFailed
			st.Error = "empty proof from prover"
		})
		p.removeJob(jobID)
		return
	}

	sb, err := p.sbStore.GetSuperblock(ctx, job.number)
	if err != nil {
		p.log.Error().Err(err).Uint64("superblock", job.number).Msg("Failed to load superblock for proof completion")
		return
	}
	sb.Proof = append([]byte(nil), proofBytes...)

	if err := p.sbStore.StoreSuperblock(ctx, sb); err != nil {
		p.log.Error().Err(err).Uint64("superblock", job.number).Msg("Failed to persist superblock with proof")
		return
	}

	if p.publishFn != nil {
		if err := p.publishFn(ctx, sb, proofBytes); err != nil {
			p.log.Error().Err(err).Uint64("superblock", job.number).Msg("Failed to publish superblock with proof")
			_ = p.collector.UpdateStatus(ctx, job.hash, func(st *proofs.Status) {
				st.State = proofs.StateFailed
				st.Error = err.Error()
			})
			return
		}
	}

	_ = p.collector.UpdateStatus(ctx, job.hash, func(st *proofs.Status) {
		st.State = proofs.StateComplete
		st.Error = ""
	})
	p.removeJob(jobID)
	p.log.Info().Str("job_id", jobID).Uint64("superblock", job.number).Msg("Proof job completed and published")
}

func (p *proofPipeline) removeJob(jobID string) {
	p.mu.Lock()
	delete(p.jobs, jobID)
	p.mu.Unlock()
}

func (p *proofPipeline) missingChains(required []uint32, subs []proofs.Submission) []int {
	have := make(map[uint32]struct{}, len(subs))
	for _, s := range subs {
		have[s.ChainID] = struct{}{}
	}
	var out []int
	for _, id := range required {
		if _, ok := have[id]; !ok {
			out = append(out, int(id))
		}
	}
	return out
}

func (p *proofPipeline) logStats() {
	p.mu.Lock()
	queued := len(p.jobs)
	jobIDs := make([]string, 0, queued)
	for id := range p.jobs {
		jobIDs = append(jobIDs, id)
	}
	p.mu.Unlock()

	if queued == 0 {
		p.log.Debug().Msg("Proof pipeline idle")
		return
	}

	p.log.Info().
		Int("outstanding_jobs", queued).
		Strs("job_ids", jobIDs).
		Msg("Active proof jobs awaiting completion")
}
