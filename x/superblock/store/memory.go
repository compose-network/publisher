// Package store. This memory store is for testing purposes only. True production store will be implemented later.
// So im adding a TODO comment here.
package store

import (
	"context"
	"fmt"
	"sync"
)

// MemorySuperblockStore provides in-memory storage for superblocks.
type MemorySuperblockStore struct {
	mu          sync.RWMutex
	superblocks map[uint64]*Superblock // key: superblock number
	byHash      map[string]*Superblock // key: hex hash
	latest      *Superblock
}

func NewMemorySuperblockStore() *MemorySuperblockStore {
	return &MemorySuperblockStore{
		superblocks: make(map[uint64]*Superblock),
		byHash:      make(map[string]*Superblock),
	}
}

func (s *MemorySuperblockStore) StoreSuperblock(_ context.Context, superblock *Superblock) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.superblocks[superblock.Number] = superblock
	s.byHash[fmt.Sprintf("%x", superblock.Hash)] = superblock

	if s.latest == nil || superblock.Number > s.latest.Number {
		s.latest = superblock
	}

	return nil
}

func (s *MemorySuperblockStore) GetSuperblock(_ context.Context, number uint64) (*Superblock, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if superblock, exists := s.superblocks[number]; exists {
		return superblock, nil
	}
	return nil, fmt.Errorf("superblock not found: %d", number)
}

func (s *MemorySuperblockStore) GetSuperblockByHash(_ context.Context, hash []byte) (*Superblock, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hashStr := fmt.Sprintf("%x", hash)
	if superblock, exists := s.byHash[hashStr]; exists {
		return superblock, nil
	}
	return nil, fmt.Errorf("superblock not found: %x", hash)
}

func (s *MemorySuperblockStore) GetLatestSuperblock(context.Context) (*Superblock, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.latest == nil {
		return nil, fmt.Errorf("no superblocks found")
	}
	return s.latest, nil
}

func (s *MemorySuperblockStore) GetSuperblockCount(context.Context) (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return uint64(len(s.superblocks)), nil
}

func (s *MemorySuperblockStore) DeleteSuperblock(_ context.Context, number uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if superblock, exists := s.superblocks[number]; exists {
		delete(s.superblocks, number)
		delete(s.byHash, fmt.Sprintf("%x", superblock.Hash))

		if s.latest != nil && s.latest.Number == number {
			s.latest = nil
			for _, sb := range s.superblocks {
				if s.latest == nil || sb.Number > s.latest.Number {
					s.latest = sb
				}
			}
		}
	}

	return nil
}
