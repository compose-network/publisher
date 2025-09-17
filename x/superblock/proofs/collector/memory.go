package collector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"

	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs"
)

var _ Service = (*Memory)(nil)

// Memory implements an in-memory collector; suitable for tests and single-instance deployments.
type Memory struct {
	mu       sync.RWMutex
	bySB     map[string]map[uint32]proofs.Submission
	statuses map[string]proofs.Status
	log      zerolog.Logger
}

// NewMemory returns a configured Memory collector.
func NewMemory(log zerolog.Logger) *Memory {
	logger := log.With().Str("component", "proof-collector").Logger()

	m := &Memory{
		bySB:     make(map[string]map[uint32]proofs.Submission),
		statuses: make(map[string]proofs.Status),
		log:      logger,
	}

	logger.Info().Msg("Memory proof collector initialized")
	return m
}

func (m *Memory) SubmitOpSuccinct(_ context.Context, s proofs.Submission) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s.SuperblockHash == (common.Hash{}) {
		m.log.Error().Msg("submission rejected: superblock hash is required")
		return fmt.Errorf("superblock hash is required")
	}
	if s.ReceivedAt.IsZero() {
		s.ReceivedAt = time.Now()
	}

	key := s.SuperblockHash.Hex()
	isNewSuperblock := m.bySB[key] == nil

	if isNewSuperblock {
		m.bySB[key] = make(map[uint32]proofs.Submission)
		m.log.Info().
			Str("superblock_hash", key).
			Uint64("superblock_number", s.SuperblockNumber).
			Msg("new superblock started collecting proofs")
	}

	m.bySB[key][s.ChainID] = s

	st := m.statuses[key]
	if st.SuperblockNumber == 0 {
		st.SuperblockNumber = s.SuperblockNumber
		st.SuperblockHash = s.SuperblockHash
		st.State = "collecting"
	}
	if st.Received == nil {
		st.Received = make(map[uint32]time.Time)
	}
	st.Received[s.ChainID] = s.ReceivedAt
	m.statuses[key] = st

	m.log.Info().
		Str("superblock_hash", key).
		Uint64("superblock_number", s.SuperblockNumber).
		Uint32("chain_id", s.ChainID).
		Str("prover_address", s.ProverAddress.Hex()).
		Int("total_submissions", len(m.bySB[key])).
		Msg("proof submission collected")

	return nil
}

func (m *Memory) GetStatus(_ context.Context, sbHash common.Hash) (proofs.Status, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := sbHash.Hex()
	st, ok := m.statuses[key]
	if !ok {
		return proofs.Status{}, fmt.Errorf("unknown superblock")
	}
	return st, nil
}

func (m *Memory) ListSubmissions(_ context.Context, sbHash common.Hash) ([]proofs.Submission, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := sbHash.Hex()
	subsMap, ok := m.bySB[key]
	if !ok {
		return nil, nil
	}
	out := make([]proofs.Submission, 0, len(subsMap))
	for _, sub := range subsMap {
		out = append(out, sub)
	}
	return out, nil
}

func (m *Memory) UpdateStatus(_ context.Context, sbHash common.Hash, mutate func(*proofs.Status)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := sbHash.Hex()
	st, ok := m.statuses[key]
	if !ok {
		return fmt.Errorf("unknown superblock")
	}
	mutate(&st)
	m.statuses[key] = st
	return nil
}
