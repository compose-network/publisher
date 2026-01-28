package consensus

import (
	"context"
	"sync"
	"time"
)

// StateManager manages transaction states with automatic cleanup.
type StateManager struct {
	mu            sync.RWMutex
	states        map[string]*TransactionState
	cleanupPeriod time.Duration
	retentionTime time.Duration
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

// NewStateManager creates a new StateManager.
func NewStateManager(cleanupPeriod, retentionTime time.Duration) *StateManager {
	return &StateManager{
		states:        make(map[string]*TransactionState),
		cleanupPeriod: cleanupPeriod,
		retentionTime: retentionTime,
		stopCh:        make(chan struct{}),
	}
}

// Start begins the background cleanup goroutine.
func (m *StateManager) Start(ctx context.Context) {
	m.wg.Add(1)
	go m.cleanupLoop(ctx)
}

// Stop stops the cleanup goroutine.
func (m *StateManager) Stop() {
	close(m.stopCh)
	m.wg.Wait()
}

// AddState creates a new transaction state.
func (m *StateManager) AddState(id string, participantChains []uint64, data []byte) *TransactionState {
	m.mu.Lock()
	defer m.mu.Unlock()

	state := &TransactionState{
		ID:                id,
		ParticipantChains: participantChains,
		Votes:             make(map[uint64]bool),
		Decision:          StatePending,
		Data:              data,
		StartTime:         time.Now(),
	}

	m.states[id] = state
	return state
}

// GetState retrieves a transaction state by ID.
func (m *StateManager) GetState(id string) *TransactionState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.states[id]
}

// RemoveState removes a transaction state by ID.
func (m *StateManager) RemoveState(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, id)
}

// GetAllPending returns all pending transaction IDs.
func (m *StateManager) GetAllPending() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var ids []string
	for id, state := range m.states {
		if state.Decision == StatePending {
			ids = append(ids, id)
		}
	}
	return ids
}

// GetAll returns all transaction IDs.
func (m *StateManager) GetAll() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.states))
	for id := range m.states {
		ids = append(ids, id)
	}
	return ids
}

// AddVote adds a vote to a transaction. Returns false if already voted.
func (m *StateManager) AddVote(id string, chainID uint64, vote bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.states[id]
	if !ok {
		return false
	}

	// Check if already voted
	if _, exists := state.Votes[chainID]; exists {
		return false
	}

	state.Votes[chainID] = vote
	return true
}

// SetDecision sets the decision for a transaction.
func (m *StateManager) SetDecision(id string, decision DecisionState) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.states[id]
	if !ok {
		return false
	}

	if state.Decision != StatePending {
		return false
	}

	state.Decision = decision
	state.DecidedTime = time.Now()
	return true
}

// GetStats returns statistics about managed states.
func (m *StateManager) GetStats() (total, pending, committed, aborted int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total = len(m.states)
	for _, state := range m.states {
		switch state.Decision {
		case StatePending:
			pending++
		case StateCommit:
			committed++
		case StateAbort:
			aborted++
		}
	}
	return
}

func (m *StateManager) cleanupLoop(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(m.cleanupPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.cleanup()
		}
	}
}

func (m *StateManager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-m.retentionTime)
	for id, state := range m.states {
		if state.IsDecided() && state.DecidedTime.Before(cutoff) {
			delete(m.states, id)
		}
	}
}
