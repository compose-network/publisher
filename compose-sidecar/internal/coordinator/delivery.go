package coordinator

import (
	"context"
	"fmt"

	"github.com/compose-network/compose-sdk/protocol"
	"github.com/compose-network/compose-sidecar/internal/types"
	"github.com/compose-network/specs/compose"
)

// deliverableXT holds the data needed to build builder transactions for a
// single committed XT on a specific chain.
type deliverableXT struct {
	id     string
	xt     *types.PendingXT
	rawTxs [][]byte
	deps   []protocol.CrossRollupDependency
}

// collectDeliverable walks entries in order, collecting committed XTs that
// have not yet been delivered to the given chain. It returns both the
// deliverable prefix and the first undecided blocking entry (if any).
func (c *DefaultCoordinator) collectDeliverable(
	chainID compose.ChainID,
	entries []*pendingXTEntry,
) ([]deliverableXT, *pendingXTEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var deliverable []deliverableXT
	for _, entry := range entries {
		xt := entry.xt
		if xt.DeliveredChains != nil && xt.DeliveredChains[chainID] {
			continue
		}
		if xt.Decision == nil {
			return deliverable, entry
		}
		if !*xt.Decision {
			if xt.DeliveredChains == nil {
				xt.DeliveredChains = make(map[compose.ChainID]bool)
			}
			xt.DeliveredChains[chainID] = true
			continue
		}
		rawTxs, ok := xt.RawTxs[chainID]
		if !ok || len(rawTxs) == 0 {
			continue
		}
		deliverable = append(deliverable, deliverableXT{
			id:     entry.id,
			xt:     xt,
			rawTxs: rawTxs,
			deps:   depsForChain(xt.FulfilledDeps, chainID),
		})
	}

	return deliverable, nil
}

// buildCommittedTransactions prepares TransactionPayload entries for the builder,
// including putInbox transactions for fulfilled CIRC dependencies.
func (c *DefaultCoordinator) buildCommittedTransactions(
	ctx context.Context,
	chainID compose.ChainID,
	deliverable []deliverableXT,
) ([]protocol.TransactionPayload, error) {
	if len(deliverable) == 0 {
		return nil, nil
	}

	totalDeps := 0
	for _, entry := range deliverable {
		totalDeps += len(entry.deps)
	}

	var nextNonce uint64
	if totalDeps > 0 {
		if c.putInboxBuilder == nil {
			return nil, fmt.Errorf("putInbox builder not configured")
		}
		startNonce, err := c.nonceManager.Reserve(ctx, totalDeps, c.putInboxBuilder.PendingNonceAt)
		if err != nil {
			return nil, err
		}
		nextNonce = startNonce
	}

	txs := make([]protocol.TransactionPayload, 0, len(deliverable)+totalDeps)
	for _, entry := range deliverable {
		for _, dep := range entry.deps {
			putInboxTx, err := c.putInboxBuilder.BuildPutInboxTxWithNonce(ctx, dep, nextNonce)
			if err != nil {
				return nil, err
			}
			nextNonce++
			putInboxBytes, err := putInboxTx.MarshalBinary()
			if err != nil {
				return nil, err
			}
			txs = append(txs, protocol.TransactionPayload{
				Raw:        fmt.Sprintf("0x%x", putInboxBytes),
				Required:   true,
				InstanceID: entry.id,
			})
		}
		for _, rawTx := range entry.rawTxs {
			txs = append(txs, protocol.TransactionPayload{
				Raw:        fmt.Sprintf("0x%x", rawTx),
				Required:   true,
				InstanceID: entry.id,
			})
		}
	}

	return txs, nil
}

// markDelivered marks the given deliverable XTs as delivered for the chain.
// Delivered XTs are not returned on subsequent polls.
func (c *DefaultCoordinator) markDelivered(chainID compose.ChainID, deliverable []deliverableXT) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, entry := range deliverable {
		if entry.xt.DeliveredChains == nil {
			entry.xt.DeliveredChains = make(map[compose.ChainID]bool)
		}
		entry.xt.DeliveredChains[chainID] = true
	}
}

// depsForChain filters dependencies to only those targeting the given chain.
func depsForChain(deps []protocol.CrossRollupDependency, chainID compose.ChainID) []protocol.CrossRollupDependency {
	if len(deps) == 0 {
		return nil
	}
	filtered := make([]protocol.CrossRollupDependency, 0, len(deps))
	for _, dep := range deps {
		if dep.DestChainID == chainID {
			filtered = append(filtered, dep)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}
