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
	states map[compose.InstanceID]*TwoPCState

	// Cleanup management
	cleanupInterval time.Duration
	stopCleanup     chan struct{}
	cleanupWg       sync.WaitGroup
}

// NewStateManager creates a new state manager
func NewStateManager() *StateManager {
	sm := &StateManager{
		states:          make(map[compose.InstanceID]*TwoPCState),
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
	instanceID compose.InstanceID, req *pb.XTRequest, chains map[compose.ChainID]struct{},
) (*TwoPCState, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.states[instanceID]; exists {
		return nil, fmt.Errorf("transaction %s already exists", instanceID.String())
	}

	state := NewTwoPCState(instanceID, req, chains)
	sm.states[instanceID] = state

	return state, nil
}

// GetState retrieves a transaction state
func (sm *StateManager) GetState(instanceID compose.InstanceID) (*TwoPCState, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	state, exists := sm.states[instanceID]
	return state, exists
}

// RemoveState removes a transaction state
func (sm *StateManager) RemoveState(instanceID compose.InstanceID) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if state, exists := sm.states[instanceID]; exists {
		if state.Timer != nil {
			state.Timer.Stop()
		}
		delete(sm.states, instanceID)
	}
}

// GetAllActiveIDs returns all active transaction IDs
func (sm *StateManager) GetAllActiveIDs() []compose.InstanceID {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	ids := make([]compose.InstanceID, 0, len(sm.states))
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
	toRemove := make([]compose.InstanceID, 0)

	for instanceID, state := range sm.states {
		if state.IsComplete() && now.Sub(state.StartTime) > 10*time.Minute {
			toRemove = append(toRemove, instanceID)
		}
	}

	for _, instanceID := range toRemove {
		if state := sm.states[instanceID]; state != nil {
			if state.Timer != nil {
				state.Timer.Stop()
			}
			delete(sm.states, instanceID)
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
	sm.states = make(map[compose.InstanceID]*TwoPCState)
	sm.mu.Unlock()
}
