package mempool

import (
	"encoding/hex"

	pb "github.com/compose-network/publisher/proto/rollup/v1"
)

// Filter applies state-based transaction filtering.
// During submission state, only transactions in the inclusion list are allowed.
type Filter struct{}

// NewFilter creates a transaction filter.
func NewFilter() *Filter {
	return &Filter{}
}

// Apply filters records based on sequencer state and seal request inclusion.
func (f *Filter) Apply(
	records []*Record,
	state State,
	sealRequest *pb.RequestSeal,
) []*Record {
	if state != StateSubmission {
		return records
	}

	if sealRequest == nil || len(sealRequest.IncludedXts) == 0 {
		return records
	}

	// Build inclusion set from seal request
	included := make(map[string]bool, len(sealRequest.IncludedXts))
	for _, xtBytes := range sealRequest.IncludedXts {
		included[hex.EncodeToString(xtBytes)] = true
	}

	// Filter records: keep if no xtID or if xtID is in inclusion list
	filtered := make([]*Record, 0, len(records))
	for _, rec := range records {
		if rec.XtID == "" {
			filtered = append(filtered, rec)
			continue
		}

		if included[rec.XtID] {
			filtered = append(filtered, rec)
		}
	}

	return filtered
}
