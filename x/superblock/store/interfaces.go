package store

import (
	"context"
)

type SuperblockStore interface {
	StoreSuperblock(ctx context.Context, superblock *Superblock) error
	GetSuperblock(ctx context.Context, number uint64) (*Superblock, error)
	GetSuperblockByHash(ctx context.Context, hash []byte) (*Superblock, error)
	GetLatestSuperblock(ctx context.Context) (*Superblock, error)
	GetSuperblockCount(ctx context.Context) (uint64, error)
	DeleteSuperblock(ctx context.Context, number uint64) error
}
