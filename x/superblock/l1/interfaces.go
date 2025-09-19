package l1

import (
	"context"

	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs"

	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/l1/events"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/l1/tx"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/store"
)

type Publisher interface {
	PublishSuperblockWithProof(
		ctx context.Context,
		superblock *store.Superblock,
		proof []byte,
		outputs *proofs.SuperblockAggOutputs,
	) (*tx.Transaction, error)
	GetPublishStatus(ctx context.Context, txHash []byte) (*tx.TransactionStatus, error)
	WatchSuperblocks(ctx context.Context) (<-chan *events.SuperblockEvent, error)
	GetLatestL1Block(ctx context.Context) (*BlockInfo, error)
}
