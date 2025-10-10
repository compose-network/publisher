package wal

import (
	"context"
	"sync"
	"time"
)

// NewMemoryManager creates an in-memory WAL manager.
func NewMemoryManager() Manager {
	return &memoryManager{
		entries: make([]*Entry, 0),
	}
}

type memoryManager struct {
	mu      sync.RWMutex
	entries []*Entry
	nextID  uint64
}

func (m *memoryManager) WriteEntry(_ context.Context, entry *Entry) error {
	if entry == nil {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.nextID++
	cp := *entry
	cp.ID = m.nextID
	if cp.Timestamp.IsZero() {
		cp.Timestamp = time.Now()
	}

	m.entries = append(m.entries, &cp)
	return nil
}

func (m *memoryManager) ReadEntries(_ context.Context, checkpoint uint64) ([]*Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make([]*Entry, 0, len(m.entries))
	for _, e := range m.entries {
		if e.ID > checkpoint {
			cp := *e
			results = append(results, &cp)
		}
	}
	return results, nil
}

func (m *memoryManager) Checkpoint(_ context.Context, checkpoint uint64) error {
	// No-op for in-memory manager.
	return nil
}

func (m *memoryManager) Truncate(_ context.Context, beforeSlot uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	filtered := m.entries[:0]
	for _, e := range m.entries {
		if e.Slot >= beforeSlot {
			filtered = append(filtered, e)
		}
	}
	m.entries = filtered
	return nil
}

func (m *memoryManager) Close() error {
	return nil
}
