package periodrunner

import "time"

const (
	// DefaultEpochsPerPeriod is the number of Ethereum epochs that compose one period
	DefaultEpochsPerPeriod = 10
	EthSlotsPerEpoch       = uint64(32)
	EthSlotDuration        = 12 * time.Second
)

var (
	// DefaultGenesisTime represents a default genesis time as in 2025-10-29 00:00:00 UTC
	DefaultGenesisTime = time.Date(2025, 10, 29, 0, 0, 0, 0, time.UTC)
)
