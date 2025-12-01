package mempool

// Policy enforces nonce gap rules for sequencer-managed transactions.
// Transactions with unfillable gaps are pruned after expiry to prevent mempool bloat.
type Policy struct {
	maxFutureNonceGap   uint64
	nonceGapExpirySlots uint64
}

// NewPolicy creates a nonce gap policy with the given configuration.
func NewPolicy(config Config) *Policy {
	return &Policy{
		maxFutureNonceGap:   config.MaxFutureNonceGap,
		nonceGapExpirySlots: config.NonceGapExpirySlots,
	}
}

// ShouldPrune determines if a transaction with a nonce gap should be removed.
// Transactions are pruned if they exceed the gap expiry slot threshold.
func (p *Policy) ShouldPrune(rec *Record, currentSlot uint64) bool {
	if rec.Status != StatusStaged {
		return false
	}

	slotAge := currentSlot - rec.CreatedSlot
	return slotAge >= p.nonceGapExpirySlots
}
