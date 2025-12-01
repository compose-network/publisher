package mempool

import (
	"context"
	"fmt"

	pb "github.com/compose-network/publisher/proto/rollup/v1"
)

// Manager coordinates the transaction lifecycle for sequencers.
// It maintains the transaction state, applies ordering rules, and filters
// transactions based on seal request inclusion lists.
type Manager struct {
	tracker  *Tracker
	filter   *Filter
	ordering *Ordering
	policy   *Policy
}

// NewManager creates a Manager with the given configuration.
func NewManager(config Config) *Manager {
	return &Manager{
		tracker:  NewTracker(),
		filter:   NewFilter(),
		ordering: NewOrdering(),
		policy:   NewPolicy(config),
	}
}

// Config holds mempool policy configuration.
type Config struct {
	MaxFutureNonceGap   uint64
	NonceGapExpirySlots uint64
}

// DefaultConfig returns production-ready defaults for 12-second slot timing.
func DefaultConfig() Config {
	return Config{
		MaxFutureNonceGap:   1,
		NonceGapExpirySlots: 6,
	}
}

// StageTransaction adds a transaction in staged status.
func (m *Manager) StageTransaction(
	ctx context.Context,
	hash string,
	xtID string,
	kind TxKind,
	nonce uint64,
	from []byte,
	currentSlot uint64,
) error {
	return m.tracker.Add(hash, xtID, kind, nonce, from, currentSlot)
}

// MarkCommitted transitions staged transactions to committed status.
// Called after transactions are included in a block.
func (m *Manager) MarkCommitted(
	ctx context.Context,
	slot uint64,
	blockNumber uint64,
	hashes []string,
) error {
	return m.tracker.MarkCommitted(slot, blockNumber, hashes)
}

// MarkDelivered removes finalized transactions from tracking.
func (m *Manager) MarkDelivered(ctx context.Context, hashes []string) error {
	return m.tracker.MarkDelivered(hashes)
}

// GetOrderedHashesForBlock returns transaction hashes ready for block inclusion.
// Transactions are ordered with putInbox before original, grouped by cross-chain transaction.
// Filtering is applied based on sequencer state and seal request inclusion list.
func (m *Manager) GetOrderedHashesForBlock(
	ctx context.Context,
	state State,
	sealRequest *pb.RequestSeal,
	shouldHoldTx func(hash string, nonce uint64, from []byte) bool,
) ([]string, error) {
	records := m.tracker.GetByStatus(StatusStaged)
	filtered := m.filter.Apply(records, state, sealRequest)
	bundles := m.ordering.BuildBundles(filtered)

	ready := make([]string, 0, len(bundles))
	for _, bundle := range bundles {
		for _, hash := range bundle.Hashes {
			rec := m.tracker.Get(hash)
			if rec == nil {
				continue
			}

			if shouldHoldTx != nil && shouldHoldTx(hash, rec.Nonce, rec.From) {
				continue
			}

			ready = append(ready, hash)
		}
	}

	return ready, nil
}

// AssignXtID associates a transaction with a cross-chain transaction ID.
// Used when the xtID is determined after initial staging.
func (m *Manager) AssignXtID(hash string, xtID string) error {
	return m.tracker.AssignXtID(hash, xtID)
}

// ClearByXtID removes all transactions for an aborted cross-chain transaction.
func (m *Manager) ClearByXtID(ctx context.Context, xtID string) error {
	return m.tracker.ClearByXtID(xtID)
}

// PruneGapped removes transactions with expired nonce gaps.
func (m *Manager) PruneGapped(ctx context.Context, currentSlot uint64) error {
	records := m.tracker.All()
	toPrune := make([]string, 0)

	for _, rec := range records {
		if m.policy.ShouldPrune(rec, currentSlot) {
			toPrune = append(toPrune, rec.Hash)
		}
	}

	if len(toPrune) > 0 {
		return m.tracker.Remove(toPrune)
	}

	return nil
}

// GetRecordsByXtID returns all records for a cross-chain transaction.
func (m *Manager) GetRecordsByXtID(xtID string) []*Record {
	return m.tracker.GetByXtID(xtID)
}

// Stats returns current mempool statistics.
func (m *Manager) Stats() Stats {
	all := m.tracker.All()

	stats := Stats{}
	for _, rec := range all {
		switch rec.Status {
		case StatusStaged:
			stats.Staged++
		case StatusCommitted:
			stats.Committed++
		}

		switch rec.Kind {
		case KindPutInbox:
			stats.PutInbox++
		case KindOriginal:
			stats.Original++
		case KindCIRCDelivery:
			// CIRC delivery transactions not counted separately
		}
	}

	return stats
}

// Stats holds mempool statistics.
type Stats struct {
	Staged    int
	Committed int
	PutInbox  int
	Original  int
}

func (s Stats) String() string {
	return fmt.Sprintf("staged=%d committed=%d putInbox=%d original=%d",
		s.Staged, s.Committed, s.PutInbox, s.Original)
}
