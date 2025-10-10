package slot

import (
	"context"
	"time"
)

// Slot handles 12-second Ethereum-aligned slots for SBCP coordination.
// It provides slot timing, progress tracking, and seal time calculations.
type Slot interface {
	// GetCurrent returns the current slot number based on genesis time
	GetCurrent() uint64

	// GetStartTime returns the start time for a given slot
	GetStartTime(slot uint64) time.Time

	// GetProgress returns the progress through the current slot (0.0 to 1.0)
	GetProgress() float64

	// IsSealTime returns true if we've reached the seal cutover point
	IsSealTime() bool

	// WaitForNext blocks until the next slot begins
	WaitForNext(ctx context.Context) error

	// SetGenesisTime updates the genesis time
	SetGenesisTime(genesis time.Time)

	// GetSealTime returns the seal cutover time for a given slot
	GetSealTime(slot uint64) time.Time

	// GetEndTime returns the end time for a given slot
	GetEndTime(slot uint64) time.Time

	// TimeUntilSeal returns duration until the current slot's seal time
	TimeUntilSeal() time.Duration

	// TimeUntilEnd returns duration until the current slot ends
	TimeUntilEnd() time.Duration
}
