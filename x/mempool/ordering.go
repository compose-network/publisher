package mempool

import "sort"

// Ordering builds transaction bundles respecting cross-chain ordering rules.
// For each cross-chain transaction, putInbox must precede original.
// All putInbox transactions are ordered by nonce before all originals.
type Ordering struct{}

// NewOrdering creates a bundle ordering coordinator.
func NewOrdering() *Ordering {
	return &Ordering{}
}

// BuildBundles groups transactions into ordered bundles.
// Returns bundles ready for block inclusion.
func (o *Ordering) BuildBundles(records []*Record) []Bundle {
	if len(records) == 0 {
		return nil
	}

	// Separate records by whether they have an xtID
	withoutXtID := make([]*Record, 0)
	groups := make(map[string]*group)

	for _, rec := range records {
		if rec.XtID == "" {
			withoutXtID = append(withoutXtID, rec)
			continue
		}

		g, ok := groups[rec.XtID]
		if !ok {
			g = &group{xtID: rec.XtID}
			groups[rec.XtID] = g
		}

		if rec.Kind == KindPutInbox {
			g.putInbox = append(g.putInbox, rec)
		} else {
			g.originals = append(g.originals, rec)
		}
	}

	// Build bundles for standalone transactions
	bundles := make([]Bundle, 0, len(withoutXtID)+len(groups))
	for _, rec := range withoutXtID {
		bundles = append(bundles, Bundle{
			XtID:   "",
			Hashes: []string{rec.Hash},
		})
	}

	// Collect all putInbox and original transactions across all XTs
	allPutInbox := make([]*Record, 0)
	allOriginals := make([]*Record, 0)

	for _, g := range groups {
		allPutInbox = append(allPutInbox, g.putInbox...)
		allOriginals = append(allOriginals, g.originals...)
	}

	// Sort by nonce to maintain execution order
	sort.Slice(allPutInbox, func(i, j int) bool {
		return allPutInbox[i].Nonce < allPutInbox[j].Nonce
	})

	sort.Slice(allOriginals, func(i, j int) bool {
		return allOriginals[i].Nonce < allOriginals[j].Nonce
	})

	// All putInbox transactions before all originals
	for _, rec := range allPutInbox {
		bundles = append(bundles, Bundle{
			XtID:   rec.XtID,
			Hashes: []string{rec.Hash},
		})
	}

	for _, rec := range allOriginals {
		bundles = append(bundles, Bundle{
			XtID:   rec.XtID,
			Hashes: []string{rec.Hash},
		})
	}

	return bundles
}

type group struct {
	xtID      string
	putInbox  []*Record
	originals []*Record
}
