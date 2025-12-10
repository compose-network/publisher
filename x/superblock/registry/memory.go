package registry

import (
	"context"
	"sync"
	"time"

	"github.com/compose-network/specs/compose"
	"github.com/rs/zerolog"
)

// memoryService is a simple in-memory registry for dev/testing.
// It satisfies Service and returns a static set of active rollups.
type memoryService struct {
	mu       sync.RWMutex
	chainIDs []compose.ChainID
	log      zerolog.Logger

	eventCh chan Event
}

func NewMemoryService(log zerolog.Logger, chainIDs []compose.ChainID) Service {
	ids := make([]compose.ChainID, 0, len(chainIDs))

	for _, chainID := range chainIDs {
		if chainID == 0 {
			continue
		}
		ids = append(ids, chainID)
	}

	return &memoryService{
		chainIDs: ids,
		log:      log.With().Str("component", "registry.memory").Logger(),
		eventCh:  make(chan Event, 1),
	}
}

func (m *memoryService) Start(context.Context) error { return nil }
func (m *memoryService) Stop(context.Context) error  { return nil }

func (m *memoryService) GetActiveRollups(context.Context) ([]compose.ChainID, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]compose.ChainID, len(m.chainIDs))
	copy(out, m.chainIDs)

	return out, nil
}

func (m *memoryService) GetRollupEndpoint(context.Context, compose.ChainID) (string, error) {
	return "", nil
}
func (m *memoryService) GetRollupPublicKey(context.Context, compose.ChainID) ([]byte, error) {
	return nil, nil
}
func (m *memoryService) IsRollupActive(context.Context, compose.ChainID) (bool, error) {
	return true, nil
}

func (m *memoryService) WatchRegistry(context.Context) (<-chan Event, error) {
	// Never emits in-memory changes; return channel for API compatibility.
	return m.eventCh, nil
}

func (m *memoryService) GetRollupInfo(compose.ChainID) (*RollupInfo, error) { return nil, nil }
func (m *memoryService) GetAllRollups() map[compose.ChainID]*RollupInfo {
	return map[compose.ChainID]*RollupInfo{}
}
func (m *memoryService) SetPollingInterval(time.Duration) {}
