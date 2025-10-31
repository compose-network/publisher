package periodrunner

import (
	"time"

	"github.com/rs/zerolog"
)

// PeriodRunnerConfig configures a PeriodRunner.
type PeriodRunnerConfig struct {
	// Handler is the function invoked whenever a new SBCP period starts.
	Handler PeriodCallback
	// EpochsPerPeriod is the number of Ethereum epochs in one SBCP period.
	EpochsPerPeriod uint64
	// GenesisTime is the timestamp at which period 0 starts.
	GenesisTime time.Time
	// Now returns the current time. Useful for deterministic tests. Defaults to time.Now if nil.
	Now    func() time.Time
	Logger zerolog.Logger
}

// DefaultPeriodRunnerConfig returns a config with sensible defaults.
func DefaultPeriodRunnerConfig(logger zerolog.Logger) PeriodRunnerConfig {
	return PeriodRunnerConfig{
		Handler:         nil, // Set later by an upper layer
		EpochsPerPeriod: DefaultEpochsPerPeriod,
		GenesisTime:     DefaultGenesisTime,
		Now:             time.Now,
		Logger:          logger.With().Str("component", "period-runner").Logger(),
	}
}

// IsEmpty returns true if all fields are at their zero values.
func (p *PeriodRunnerConfig) IsEmpty() bool {
	return p.Handler == nil &&
		p.EpochsPerPeriod == 0 &&
		p.GenesisTime.IsZero() &&
		p.Now == nil &&
		p.Logger.GetLevel() == zerolog.NoLevel
}
