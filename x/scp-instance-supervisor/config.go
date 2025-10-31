package scpsupervisor

import (
	"time"

	"github.com/compose-network/specs/compose/scp"
	"github.com/rs/zerolog"
)

// Config contains all dependencies for Supervisor.
type Config struct {
	Logger zerolog.Logger

	// Factory builds new SCP instances
	Factory SCPFactory

	// Network is provided to the Factory and used by SCP instances to send messages.
	Network scp.PublisherNetwork

	// TimerFactory creates per-instance timers for enforcing timeouts.
	TimerFactory TimerFactory

	// InstanceTimeout bounds the lifetime of an instance; 0 disables timeouts.
	InstanceTimeout time.Duration

	// Now returns the current time; defaults to time.Now.
	Now func() time.Time

	// History limits. Zero values disable each pruning mechanism.
	MaxHistory       int
	HistoryRetention time.Duration

	// OnFinalize is called after an instance finalizes.
	OnFinalize OnFinalizeHook
}

// DefaultConfig returns a config with sensible defaults for optional fields.
func DefaultConfig(logger zerolog.Logger, network scp.PublisherNetwork) Config {
	return Config{
		Logger:           logger.With().Str("component", "scp-instance-supervisor").Logger(),
		Factory:          scp.NewPublisherInstance,
		Network:          network,
		TimerFactory:     SystemTimerFactory{},
		InstanceTimeout:  DefaultInstanceTimeout,
		Now:              time.Now,
		MaxHistory:       DefaultMaxHistory,
		HistoryRetention: DefaultHistoryRetention,
		OnFinalize:       nil,
	}
}
