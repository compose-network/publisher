package mempool

import (
	"fmt"
	"sync"
)

// Tracker manages transaction records through their lifecycle.
// It provides efficient lookups by hash, xtID, and status.
type Tracker struct {
	mu       sync.RWMutex
	records  map[string]*Record
	byXtID   map[string][]string
	byStatus map[Status][]string
	nextSeq  uint64
}

// NewTracker creates an empty transaction tracker.
func NewTracker() *Tracker {
	return &Tracker{
		records:  make(map[string]*Record),
		byXtID:   make(map[string][]string),
		byStatus: make(map[Status][]string),
		nextSeq:  1,
	}
}

// Add creates a new transaction record in staged status.
func (t *Tracker) Add(
	hash string,
	xtID string,
	kind TxKind,
	nonce uint64,
	from []byte,
	currentSlot uint64,
) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.records[hash]; exists {
		return fmt.Errorf("transaction already tracked: %s", hash)
	}

	record := &Record{
		Hash:            hash,
		XtID:            xtID,
		Kind:            kind,
		Status:          StatusStaged,
		CreatedSlot:     currentSlot,
		LastUpdatedSlot: currentSlot,
		Nonce:           nonce,
		From:            from,
		Sequence:        t.nextSeq,
	}

	t.records[hash] = record
	t.byStatus[StatusStaged] = append(t.byStatus[StatusStaged], hash)

	if xtID != "" {
		t.byXtID[xtID] = append(t.byXtID[xtID], hash)
	}

	t.nextSeq++
	return nil
}

// Get retrieves a transaction record by hash.
func (t *Tracker) Get(hash string) *Record {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if rec, ok := t.records[hash]; ok {
		cpy := *rec
		return &cpy
	}
	return nil
}

// GetByStatus returns all records with the given status.
func (t *Tracker) GetByStatus(status Status) []*Record {
	t.mu.RLock()
	defer t.mu.RUnlock()

	hashes := t.byStatus[status]
	records := make([]*Record, 0, len(hashes))

	for _, hash := range hashes {
		if rec, ok := t.records[hash]; ok {
			cpy := *rec
			records = append(records, &cpy)
		}
	}

	return records
}

// GetByXtID returns all records for a cross-chain transaction.
func (t *Tracker) GetByXtID(xtID string) []*Record {
	t.mu.RLock()
	defer t.mu.RUnlock()

	hashes := t.byXtID[xtID]
	records := make([]*Record, 0, len(hashes))

	for _, hash := range hashes {
		if rec, ok := t.records[hash]; ok {
			cpy := *rec
			records = append(records, &cpy)
		}
	}

	return records
}

// All returns all tracked records.
func (t *Tracker) All() []*Record {
	t.mu.RLock()
	defer t.mu.RUnlock()

	records := make([]*Record, 0, len(t.records))
	for _, rec := range t.records {
		cpy := *rec
		records = append(records, &cpy)
	}

	return records
}

// AssignXtID associates a transaction with a cross-chain transaction ID.
func (t *Tracker) AssignXtID(hash string, xtID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	rec, ok := t.records[hash]
	if !ok {
		return fmt.Errorf("transaction not found: %s", hash)
	}

	if rec.XtID != "" && rec.XtID != xtID {
		return fmt.Errorf("transaction already assigned to xtID %s", rec.XtID)
	}

	rec.XtID = xtID
	t.byXtID[xtID] = append(t.byXtID[xtID], hash)

	return nil
}

// MarkCommitted transitions transactions from staged to committed status.
func (t *Tracker) MarkCommitted(slot uint64, blockNumber uint64, hashes []string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, hash := range hashes {
		rec, ok := t.records[hash]
		if !ok {
			continue
		}

		rec.Status = StatusCommitted
		rec.CommittedSlot = slot
		rec.CommittedBlock = blockNumber
		rec.LastUpdatedSlot = slot

		t.removeFromStatusList(StatusStaged, hash)
		t.byStatus[StatusCommitted] = append(t.byStatus[StatusCommitted], hash)
	}

	return nil
}

// MarkDelivered removes finalized transactions from tracking.
func (t *Tracker) MarkDelivered(hashes []string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, hash := range hashes {
		rec, ok := t.records[hash]
		if !ok {
			continue
		}

		t.removeFromStatusList(rec.Status, hash)

		if rec.XtID != "" {
			t.removeFromXtIDList(rec.XtID, hash)
		}

		delete(t.records, hash)
	}

	return nil
}

// ClearByXtID removes all transactions for a cross-chain transaction.
func (t *Tracker) ClearByXtID(xtID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	hashes := t.byXtID[xtID]
	if len(hashes) == 0 {
		return nil
	}

	for _, hash := range hashes {
		rec, ok := t.records[hash]
		if !ok {
			continue
		}

		t.removeFromStatusList(rec.Status, hash)
		delete(t.records, hash)
	}

	delete(t.byXtID, xtID)
	return nil
}

// Remove deletes transactions by hash.
func (t *Tracker) Remove(hashes []string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, hash := range hashes {
		rec, ok := t.records[hash]
		if !ok {
			continue
		}

		t.removeFromStatusList(rec.Status, hash)

		if rec.XtID != "" {
			t.removeFromXtIDList(rec.XtID, hash)
		}

		delete(t.records, hash)
	}

	return nil
}

// removeFromStatusList removes a hash from the status index.
func (t *Tracker) removeFromStatusList(status Status, hash string) {
	hashes := t.byStatus[status]
	for i, h := range hashes {
		if h == hash {
			t.byStatus[status] = append(hashes[:i], hashes[i+1:]...)
			return
		}
	}
}

// removeFromXtIDList removes a hash from the xtID index.
func (t *Tracker) removeFromXtIDList(xtID string, hash string) {
	hashes := t.byXtID[xtID]
	for i, h := range hashes {
		if h == hash {
			t.byXtID[xtID] = append(hashes[:i], hashes[i+1:]...)
			return
		}
	}
}
