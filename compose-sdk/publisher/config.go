package publisher

import (
	"time"

	"github.com/compose-network/compose-sdk/transport/quic"
	"github.com/compose-network/specs/compose"
)

// Config controls the shared publisher runtime behavior.
type Config struct {
	// QUIC transport configuration for sidecar connections.
	Quic quic.ServerConfig

	// PeriodDuration controls how often new StartPeriod messages are emitted.
	// 0 disables the periodic ticker (only the initial StartPeriod is sent).
	PeriodDuration time.Duration

	// InstanceTimeout is the SCP timeout applied per instance.
	InstanceTimeout time.Duration
}

// DefaultConfig returns sane defaults aligned with the Compose spec.
func DefaultConfig() Config {
	return Config{
		Quic:            quic.DefaultServerConfig(),
		PeriodDuration:  compose.PeriodDuration,
		InstanceTimeout: 3 * time.Second,
	}
}
