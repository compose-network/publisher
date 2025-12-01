package mempool

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPolicy_ShouldPrune_NotStaged(t *testing.T) {
	config := Config{
		MaxFutureNonceGap:   1,
		NonceGapExpirySlots: 6,
	}
	policy := NewPolicy(config)

	rec := &Record{
		Status:      StatusCommitted,
		CreatedSlot: 1,
	}

	// Committed transactions should never be pruned
	shouldPrune := policy.ShouldPrune(rec, 10)
	assert.False(t, shouldPrune)
}

func TestPolicy_ShouldPrune_WithinExpiry(t *testing.T) {
	config := Config{
		MaxFutureNonceGap:   1,
		NonceGapExpirySlots: 6,
	}
	policy := NewPolicy(config)

	rec := &Record{
		Status:      StatusStaged,
		CreatedSlot: 5,
	}

	// Current slot 10, created at slot 5 = age 5 < 6 expiry
	shouldPrune := policy.ShouldPrune(rec, 10)
	assert.False(t, shouldPrune)
}

func TestPolicy_ShouldPrune_AtExpiry(t *testing.T) {
	config := Config{
		MaxFutureNonceGap:   1,
		NonceGapExpirySlots: 6,
	}
	policy := NewPolicy(config)

	rec := &Record{
		Status:      StatusStaged,
		CreatedSlot: 4,
	}

	// Current slot 10, created at slot 4 = age 6 >= 6 expiry
	shouldPrune := policy.ShouldPrune(rec, 10)
	assert.True(t, shouldPrune)
}

func TestPolicy_ShouldPrune_BeyondExpiry(t *testing.T) {
	config := Config{
		MaxFutureNonceGap:   1,
		NonceGapExpirySlots: 6,
	}
	policy := NewPolicy(config)

	rec := &Record{
		Status:      StatusStaged,
		CreatedSlot: 1,
	}

	// Current slot 10, created at slot 1 = age 9 >= 6 expiry
	shouldPrune := policy.ShouldPrune(rec, 10)
	assert.True(t, shouldPrune)
}

func TestPolicy_ShouldPrune_ZeroAge(t *testing.T) {
	config := Config{
		MaxFutureNonceGap:   1,
		NonceGapExpirySlots: 6,
	}
	policy := NewPolicy(config)

	rec := &Record{
		Status:      StatusStaged,
		CreatedSlot: 10,
	}

	// Current slot 10, created at slot 10 = age 0 < 6 expiry
	shouldPrune := policy.ShouldPrune(rec, 10)
	assert.False(t, shouldPrune)
}
