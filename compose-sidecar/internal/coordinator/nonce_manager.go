package coordinator

import (
	"context"
	"sync"
)

type DeferredNonceManager struct {
	mu           sync.Mutex
	currentBlock uint64
	baseNonce    uint64
	nextNonce    uint64
	baseSet      bool
}

// NewDeferredNonceManager returns a nonce manager that assigns nonces at delivery time.
func NewDeferredNonceManager() *DeferredNonceManager {
	return &DeferredNonceManager{}
}

// ResetForBlock resets the nonce cursor when a new block number is observed.
func (m *DeferredNonceManager) ResetForBlock(blockNumber uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if blockNumber == m.currentBlock {
		return
	}

	m.currentBlock = blockNumber
	m.baseNonce = 0
	m.nextNonce = 0
	m.baseSet = false
}

// Reserve allocates a contiguous nonce range and returns the starting nonce.
func (m *DeferredNonceManager) Reserve(
	ctx context.Context,
	count int,
	fetchBase func(context.Context) (uint64, error),
) (uint64, error) {
	if count <= 0 {
		return 0, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.baseSet {
		base, err := fetchBase(ctx)
		if err != nil {
			return 0, err
		}
		m.baseNonce = base
		m.nextNonce = base
		m.baseSet = true
	}

	start := m.nextNonce
	m.nextNonce += uint64(count)
	return start, nil
}
