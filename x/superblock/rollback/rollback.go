package rollback

import (
	"context"

	pb "github.com/ssvlabs/rollup-shared-publisher/proto/rollup/v1"
	l1events "github.com/ssvlabs/rollup-shared-publisher/x/superblock/l1/events"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/store"
)

// Handler defines the interface for handling superblock rollback operations
type Handler interface {
	HandleSuperblockRollback(ctx context.Context, event *l1events.SuperblockEvent, sb *store.Superblock) error
}

// StateRecovery defines the interface for state recovery operations during rollback
type StateRecovery interface {
	FindLastValidSuperblock(ctx context.Context, rolledBackNumber uint64) (*store.Superblock, error)
	ComputeL2BlockRequests(ctx context.Context, lastValid *store.Superblock) ([]*pb.L2BlockRequest, error)
}

// TransactionRequeuer defines the interface for requeuing transactions after rollback
type TransactionRequeuer interface {
	RequeueRolledBackTransactions(ctx context.Context, slot uint64) error
}
