package publishermanager

import (
	"context"
	"errors"
	"time"

	"github.com/compose-network/publisher/x/messenger"
	periodrunner "github.com/compose-network/publisher/x/period-runner"
	scpsupervisor "github.com/compose-network/publisher/x/scp-instance-supervisor"
	"github.com/rs/zerolog"
)

// Config captures all dependencies needed to build the publisher publisherManager.
type Config struct {
	Context         context.Context
	Logger          zerolog.Logger
	Broadcaster     messenger.Broadcaster
	InstanceTimeout time.Duration
	EpochsPerPeriod uint64
}

func (cfg *Config) apply() error {
	if cfg.Logger.GetLevel() == zerolog.NoLevel {
		cfg.Logger = zerolog.Nop()
	}
	if cfg.Broadcaster == nil {
		return errors.New("publisher-publisherManager: broadcaster is required")
	}
	if cfg.EpochsPerPeriod == 0 {
		cfg.EpochsPerPeriod = periodrunner.DefaultEpochsPerPeriod
	}
	if cfg.InstanceTimeout == 0 {
		cfg.InstanceTimeout = scpsupervisor.DefaultInstanceTimeout
	}
	return nil
}
