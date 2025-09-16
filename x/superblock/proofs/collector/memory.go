package collector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs"
)

var _ Service = (*Memory)(nil)

// Memory implements an in-memory collector; suitable for tests and single-instance deployments.
type Memory struct {
	mu       sync.RWMutex
	bySB     map[string]map[uint32]proofs.Submission
	statuses map[string]proofs.Status
}

// NewMemory returns a configured Memory collector.
func NewMemory() *Memory {
	return &Memory{
		bySB:     make(map[string]map[uint32]proofs.Submission),
		statuses: make(map[string]proofs.Status),
	}
}

func (m *Memory) SubmitOpSuccinct(_ context.Context, s proofs.Submission) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s.SuperblockHash == (common.Hash{}) {
		return fmt.Errorf("superblock hash is required")
	}
	if s.ReceivedAt.IsZero() {
		s.ReceivedAt = time.Now()
	}

	key := s.SuperblockHash.Hex()
	if m.bySB[key] == nil {
		m.bySB[key] = make(map[uint32]proofs.Submission)
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
