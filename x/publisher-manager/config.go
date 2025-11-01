package publishermanager

import (
	"context"
	"errors"

	"github.com/compose-network/publisher/x/messenger"
	"github.com/rs/zerolog"
)

// Config captures all dependencies needed to build the publisher publisherManager.
type Config struct {
	Context     context.Context
	Logger      zerolog.Logger
	Broadcaster messenger.Broadcaster
}

func (cfg *Config) apply() error {
	if cfg.Logger.GetLevel() == zerolog.NoLevel {
		cfg.Logger = zerolog.Nop()
	}
	if cfg.Broadcaster == nil {
		return errors.New("publisher-publisherManager: broadcaster is required")
	}
	return nil
}
