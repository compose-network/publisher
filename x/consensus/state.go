package consensus

import (
	"fmt"
	"sync"
	"time"

	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
)

// StateManager manages transaction states with thread-safety and performance optimizations
type StateManager struct {
	mu     sync.RWMutex
	states map[string]*TwoPCState

	// Cleanup management
	cleanupInterval time.Duration
	stopCleanup     chan struct{}
	cleanupWg       sync.WaitGroup
}

// NewStateManager creates a new state manager
func NewStateManager() *StateManager {
	sm := &StateManager{
		states:          make(map[string]*TwoPCState),
		cleanupInterval: 5 * time.Minute,
		stopCleanup:     make(chan struct{}),
	}

	// Start cleanup goroutine
	sm.cleanupWg.Add(1)
	go sm.cleanupLoop()

	return sm
}

// AddState adds a new transaction state
func (sm *StateManager) AddState(
	instanceID []byte, req *pb.XTRequest, chains map[compose.ChainID]struct{},
) (*TwoPCState, error) {
	instanceIDStr := string(instanceID)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.states[instanceIDStr]; exists {
		return nil, fmt.Errorf("transaction %s already exists", instanceIDStr)
	}

	state := NewTwoPCState(instanceID, req, chains)
	sm.states[instanceIDStr] = state

	return state, nil
}

// GetState retrieves a transaction state
func (sm *StateManager) GetState(instanceID []byte) (*TwoPCState, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	state, exists := sm.states[string(instanceID)]
	return state, exists
}

// RemoveState removes a transaction state
func (sm *StateManager) RemoveState(instanceID []byte) {
	xtIDStr := string(instanceID)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if state, exists := sm.states[xtIDStr]; exists {
		if state.Timer != nil {
			state.Timer.Stop()
		}
		delete(sm.states, xtIDStr)
	}
}

// GetAllActiveIDs returns all active transaction IDs
func (sm *StateManager) GetAllActiveIDs() [][]byte {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	ids := make([][]byte, 0, len(sm.states))
	for _, state := range sm.states {
		ids = append(ids, state.InstanceID)
	}
	return ids
}

// GetStats returns state manager statistics
func (sm *StateManager) GetStats() map[string]interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stats := map[string]interface{}{
		"active_transactions": len(sm.states),
		"states_by_decision":  make(map[string]int),
	}

	decisionCounts := make(map[string]int)
	for _, state := range sm.states {
		decision := state.GetDecision().String()
		decisionCounts[decision]++
	}
	stats["states_by_decision"] = decisionCounts

	return stats
}

// cleanupLoop periodically removes completed transactions
func (sm *StateManager) cleanupLoop() {
	defer sm.cleanupWg.Done()

	ticker := time.NewTicker(sm.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sm.stopCleanup:
			return
		case <-ticker.C:
			sm.cleanup()
		}
	}
}

// cleanup removes old completed transactions
func (sm *StateManager) cleanup() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	toRemove := make([]string, 0)

	for xtIDStr, state := range sm.states {
		if state.IsComplete() && now.Sub(state.StartTime) > 10*time.Minute {
			toRemove = append(toRemove, xtIDStr)
		}
	}

	for _, xtIDStr := range toRemove {
		if state := sm.states[xtIDStr]; state != nil {
			if state.Timer != nil {
				state.Timer.Stop()
			}
			delete(sm.states, xtIDStr)
		}
	}
}

// Shutdown stops the state manager
func (sm *StateManager) Shutdown() {
	close(sm.stopCleanup)
	sm.cleanupWg.Wait()

	// Clean up all states
	sm.mu.Lock()
	for _, state := range sm.states {
		if state.Timer != nil {
			state.Timer.Stop()
		}
	}
	sm.states = make(map[string]*TwoPCState)
	sm.mu.Unlock()
}
