package sbcpcontroller

import (
	"errors"
	"time"

	"github.com/compose-network/publisher/x/superblock/queue"
	"github.com/compose-network/specs/compose"
	"github.com/compose-network/specs/compose/sbcp"
	"github.com/rs/zerolog"
)

// Config captures dependencies required to build a SBCPController.
type Config struct {
	Logger zerolog.Logger

	Publisher       sbcp.Publisher
	Queue           queue.XTRequestQueue
	InstanceStarter InstanceStarter

	Now func() time.Time
}

func DefaultConfig(
	logger zerolog.Logger,
	prover sbcp.PublisherProver,
	messenger sbcp.PublisherMessenger,
	l1 sbcp.L1,
	periodID compose.PeriodID,
	previousTargetSuperblockNumber compose.SuperblockNumber,
	lastFinalizedSuperblockNumber compose.SuperblockNumber,
	lastFinalizedSuperblockHash compose.SuperblockHash,
	proofWindow uint64,
	chains map[compose.ChainID]struct{},
) (Config, error) {
	publisher, err := sbcp.NewPublisher(
		prover,
		messenger,
		l1,
		periodID,
		previousTargetSuperblockNumber,
		lastFinalizedSuperblockNumber,
		lastFinalizedSuperblockHash,
		proofWindow,
		logger.With().Str("component", "sbcp").Logger(),
		chains,
	)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Logger:          logger.With().Str("component", "sbcp-controller").Logger(),
		Publisher:       publisher,
		Queue:           queue.NewMemoryXTRequestQueue(queue.DefaultConfig()),
		InstanceStarter: nil,
		Now:             time.Now,
	}, nil
}

func (c *Config) apply() error {
	if c.Logger.GetLevel() == zerolog.NoLevel {
		c.Logger = zerolog.Nop()
	}
	if c.Publisher == nil {
		return errors.New("sbcp-controller: publisher is required")
	}
	if c.Queue == nil {
		return errors.New("sbcp-controller: queue is required")
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return nil
}
