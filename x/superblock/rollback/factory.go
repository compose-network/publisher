package rollback

import (
	"github.com/rs/zerolog"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/queue"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/registry"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/slot"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/store"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/wal"
	"github.com/ssvlabs/rollup-shared-publisher/x/transport"
)

// CreateRollbackHandler creates a rollback handler with the provided dependencies
func CreateRollbackHandler(
	logger zerolog.Logger,
	superblockStore store.SuperblockStore,
	l2BlockStore store.L2BlockStore,
	registryService registry.Service,
	xtQueue queue.XTRequestQueue,
	transport transport.Transport,
	walManager wal.Manager,
	stateMachine *slot.StateMachine,
	slotManager SlotManager,
	execManager ExecutionManager,
) Handler {
	deps := Dependencies{
		SuperblockStore: superblockStore,
		L2BlockStore:    l2BlockStore,
		RegistryService: registryService,
		XTQueue:         xtQueue,
		Transport:       transport,
		WALManager:      walManager,
		StateMachine:    stateMachine,
		SlotManager:     slotManager,
		Logger:          logger,
	}

	manager := NewManager(deps, execManager)

	logger.Info().
		Bool("wal_enabled", walManager != nil).
		Msg("Created rollback manager")

	return manager
}
