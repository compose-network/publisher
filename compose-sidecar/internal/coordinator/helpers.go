package coordinator

import (
	"context"
	"sync"
	"time"

	"github.com/compose-network/compose-sdk/protocol"
	"github.com/compose-network/compose-sidecar/internal/types"
	"github.com/compose-network/specs/compose"
	"github.com/compose-network/specs/compose/proto"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// pendingXTEntry pairs a pending XT with its map key for sorting.
type pendingXTEntry struct {
	id string
	xt *types.PendingXT
}

// chainOverlay holds the accumulated state overlay for a chain within a
// single block/flashblock window. It is used to give subsequent simulations
// the post-state of previously committed XTs.
type chainOverlay struct {
	BlockNumber     uint64
	FlashblockIndex uint64
	Overlay         map[string]any
}

// PutInboxBuilder builds signed putInbox transactions for fulfilled dependencies.
type PutInboxBuilder interface {
	PendingNonceAt(ctx context.Context) (uint64, error)
	BuildPutInboxTxWithNonce(
		ctx context.Context,
		dep protocol.CrossRollupDependency,
		nonce uint64,
	) (*ethtypes.Transaction, error)
}

// xtLess defines the canonical ordering for pending XTs: period → sequence → id.
func xtLess(a, b *pendingXTEntry) bool {
	if a.xt.PeriodID != b.xt.PeriodID {
		return a.xt.PeriodID < b.xt.PeriodID
	}
	if a.xt.SequenceNum != b.xt.SequenceNum {
		return a.xt.SequenceNum < b.xt.SequenceNum
	}
	return a.id < b.id
}

func (c *DefaultCoordinator) signalWaiters(waiters map[compose.ChainID]chan *protocol.BuilderPollResponse) {
	for _, ch := range waiters {
		select {
		case ch <- &protocol.BuilderPollResponse{}:
		default:
		}
		close(ch)
	}
}

func (c *DefaultCoordinator) allChainsReady(xt *types.PendingXT) bool {
	for chainID := range xt.Transactions {
		if _, ok := xt.ChainStates[chainID]; !ok {
			return false
		}
	}
	return true
}

func (c *DefaultCoordinator) hasActiveInstanceLocked() bool {
	for _, xt := range c.pending {
		if xt.Decision != nil {
			continue
		}
		if txs, ok := xt.RawTxs[c.chainID]; ok && len(txs) > 0 {
			return true
		}
	}
	return false
}

func (c *DefaultCoordinator) rejectStartInstance(
	ctx context.Context,
	msg *proto.StartInstance,
	reason string,
) {
	instanceID := msg.InstanceIDHex()
	c.log.Warn().
		Str("instance_id", instanceID).
		Uint64("period_id", msg.PeriodId).
		Uint64("sequence", msg.SequenceNumber).
		Str("reason", reason).
		Msg("Rejecting StartInstance")

	var waiters map[compose.ChainID]chan *protocol.BuilderPollResponse
	var xt *types.PendingXT

	c.mu.Lock()
	xt = c.pending[instanceID]
	if xt == nil {
		xt = &types.PendingXT{
			ID:             instanceID,
			InstanceID:     msg.InstanceId,
			PeriodID:       compose.PeriodID(msg.PeriodId),
			SequenceNum:    compose.SequenceNumber(msg.SequenceNumber),
			Transactions:   make(map[compose.ChainID][]*ethtypes.Transaction),
			RawTxs:         make(map[compose.ChainID][][]byte),
			ChainStates:    make(map[compose.ChainID]*protocol.ChainState),
			StateOverrides: make(map[compose.ChainID]map[string]any),
			CreatedAt:      time.Now(),
			LockedChains:   make(map[compose.ChainID]bool),
		}
		c.pending[instanceID] = xt
		c.waiters[instanceID] = make(map[compose.ChainID]chan *protocol.BuilderPollResponse)
	}
	decision := false
	xt.Decision = &decision
	xt.DecidedAt = time.Now()
	waiters = c.waiters[instanceID]
	c.mu.Unlock()

	c.sendVote(ctx, instanceID, xt, false)
	if waiters != nil {
		c.signalWaiters(waiters)
		c.mu.Lock()
		delete(c.waiters, instanceID)
		c.mu.Unlock()
	}
}

func (c *DefaultCoordinator) getChainLock(chainID compose.ChainID) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	lock, ok := c.chainLocks[chainID]
	if !ok {
		lock = &sync.Mutex{}
		c.chainLocks[chainID] = lock
	}
	return lock
}

// resetBlockTrackingLocked clears per-chain block number tracking.
// Must be called with c.mu held.
func (c *DefaultCoordinator) resetBlockTrackingLocked() {
	for k := range c.lastKnownBlocks {
		delete(c.lastKnownBlocks, k)
	}
}

func (c *DefaultCoordinator) isPublisherConnected() bool {
	return c.publisher != nil && c.publisher.IsConnected()
}

// tryMakeDecision attempts to make a decision for an XT in v2 standalone mode
// after receiving a vote (local or from peer).
func (c *DefaultCoordinator) tryMakeDecision(ctx context.Context, instanceID string) {
	c.mu.Lock()
	xt, exists := c.pending[instanceID]
	if !exists {
		c.mu.Unlock()
		return
	}

	if xt.Decision != nil {
		c.mu.Unlock()
		return
	}

	expectedVotes := len(xt.RawTxs)

	collectedVotes := 0
	allYes := true

	if xt.LocalVote != nil {
		collectedVotes++
		if !*xt.LocalVote {
			allYes = false
		}
	}

	if xt.PeerVotes == nil {
		xt.PeerVotes = make(map[compose.ChainID]bool)
	}
	for chainID := range xt.RawTxs {
		if chainID == c.chainID {
			continue
		}
		if vote, ok := xt.PeerVotes[chainID]; ok {
			collectedVotes++
			if !vote {
				allYes = false
			}
		}
	}

	c.log.Debug().
		Str("instance_id", instanceID).
		Int("expected", expectedVotes).
		Int("collected", collectedVotes).
		Bool("all_yes", allYes).
		Msg("Checking decision status")

	if collectedVotes < expectedVotes {
		c.mu.Unlock()
		return
	}

	decision := allYes
	xt.Decision = &decision
	xt.DecidedAt = time.Now()

	waiters := c.waiters[instanceID]
	c.mu.Unlock()

	c.applyCommittedOverrides(instanceID)

	c.log.Info().
		Str("instance_id", instanceID).
		Bool("decision", decision).
		Int("votes", collectedVotes).
		Msg("Made local decision (v2 standalone mode)")

	c.signalWaiters(waiters)
	c.mu.Lock()
	delete(c.waiters, instanceID)
	c.mu.Unlock()
}
