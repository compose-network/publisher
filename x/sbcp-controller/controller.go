package sbcpcontroller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	scpsupervisor "github.com/compose-network/publisher/x/scp-instance-supervisor"
	"github.com/compose-network/publisher/x/superblock/queue"
	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/compose-network/specs/compose/sbcp"
	"github.com/rs/zerolog"
)

var (
	ErrNilRequest = errors.New("sbcp-sbcpController: nil xt request")
	ErrStopped    = errors.New("sbcp-sbcpController: stopped")
)

// sbcpController implements the SBCPController interface
type sbcpController struct {
	mu        sync.RWMutex
	stopped   bool
	logger    zerolog.Logger
	publisher sbcp.Publisher
	queue     queue.XTRequestQueue
	starter   InstanceStarter
	now       func() time.Time
}

// New constructs a SBCPController using the provided config.
func New(cfg Config) (SBCPController, error) {
	if err := cfg.apply(); err != nil {
		return nil, err
	}

	return &sbcpController{
		logger:    cfg.Logger,
		publisher: cfg.Publisher,
		queue:     cfg.Queue,
		starter:   cfg.InstanceStarter,
		now:       cfg.Now,
	}, nil
}

func (c *sbcpController) SetInstanceStarter(starter InstanceStarter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.starter = starter
}

// OnNewPeriod should be called whenever a new SBCP period starts.
func (c *sbcpController) OnNewPeriod(ctx context.Context) error {
	if c.isStopped() {
		return ErrStopped
	}

	if err := c.publisher.StartPeriod(); err != nil {
		c.logger.Warn().Err(err).Msg("failed to start period")
		return fmt.Errorf("sbcp start period: %w", err)
	}
	return c.TryProcessQueue(ctx)
}

// EnqueueXTRequest adds an XT request to the processing queue.
func (c *sbcpController) EnqueueXTRequest(ctx context.Context, req *pb.XTRequest, from string) error {
	if req == nil {
		return ErrNilRequest
	}
	if c.isStopped() {
		return ErrStopped
	}

	now := c.now()
	queued := &queue.QueuedXTRequest{
		Request:     clonePBXTRequest(req),
		Priority:    now.UnixNano(),
		SubmittedAt: now,
		Attempts:    0,
		From:        from,
	}

	// Set expiration time if configured
	if cfg := c.queue.Config(); cfg.RequestExpiration > 0 {
		queued.ExpiresAt = now.Add(cfg.RequestExpiration)
	}

	// Add to queue and try to progress queue
	if err := c.queue.Enqueue(ctx, queued); err != nil {
		return fmt.Errorf("enqueue xt request: %w", err)
	}
	return c.TryProcessQueue(ctx)
}

// TryProcessQueue attempts to start a queued XT request.
func (c *sbcpController) TryProcessQueue(ctx context.Context) error {
	if c.isStopped() {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Remove any expired requests first
	if cfg := c.queue.Config(); cfg.RequestExpiration > 0 {
		if _, err := c.queue.RemoveExpired(ctx); err != nil {
			return fmt.Errorf("remove expired requests: %w", err)
		}
	}

	for {
		if c.isStopped() {
			return nil
		}

		peek, err := c.queue.Peek(ctx)
		if err != nil {
			return fmt.Errorf("peek queue: %w", err)
		}
		// Remove nil entries
		if peek == nil {
			return nil
		}
		if peek.Request == nil {
			_, _ = c.queue.Dequeue(ctx)
			continue
		}
		composeReq := protoXTRequestToCompose(peek.Request)
		if composeReq == nil {
			_, _ = c.queue.Dequeue(ctx)
			continue
		}
		// Try to start instance
		instance, err := c.publisher.StartInstance(*composeReq)
		if err != nil {
			// If chains are active, leave in queue and stop processing
			if errors.Is(err, sbcp.ErrCannotStartInstance) {
				return nil
			}
			// Otherwise (unknown error), drop request and continue
			c.logger.Error().Err(err).Msg("SBCP rejected XT request")
			_, _ = c.queue.Dequeue(ctx)
			continue
		}

		// Remove from queue now that we're ready to start the instance
		queued, err := c.queue.Dequeue(ctx)
		if err != nil {
			return fmt.Errorf("dequeue request: %w", err)
		}

		if c.starter == nil {
			c.logger.Warn().Str("instance_id", instance.ID.String()).Msg("no instance starter configured; dropping request")
			continue
		}

		// Call instance starter
		if err := c.starter.StartInstance(ctx, queued, instance); err != nil {
			// If error indicates that it should be requeued, do so
			if shouldRequeueOnError(err) {
				if rErr := c.queue.Requeue(ctx, queued); rErr != nil {
					c.logger.Error().Err(rErr).Msg("failed to requeue after conflict")
				}
				return nil
			}
			// Otherwise, log and continue
			c.logger.Error().Err(err).Str("instance_id", instance.ID.String()).Msg("failed to start SCP instance")
			continue
		}
	}
}

// NotifyInstanceDecided should be called when an instance has been decided.
// Then, tries to process the queue in case freed chains allow new instances to start.
func (c *sbcpController) NotifyInstanceDecided(ctx context.Context, instance compose.Instance) error {
	if err := c.publisher.DecideInstance(instance); err != nil {
		c.logger.Error().Err(err).Str("instance_id", instance.ID.String()).Msg("SBCP failed to finalize instance")
		return fmt.Errorf("decide instance: %w", err)
	}
	return c.TryProcessQueue(ctx)
}

// AdvanceSettledState advances the settled superblock state.
func (c *sbcpController) AdvanceSettledState(superblockNumber compose.SuperblockNumber, superblockHash compose.SuperBlockHash) error {
	if err := c.publisher.AdvanceSettledState(superblockNumber, superblockHash); err != nil {
		c.logger.Error().Err(err).
			Uint64("superblock_number", uint64(superblockNumber)).
			Msg("failed to advance settled state")
		return fmt.Errorf("advance settled state: %w", err)
	}
	return nil
}

// ProofTimeout notifies the sbcpController of a proof timeout event.
func (c *sbcpController) ProofTimeout(ctx context.Context) {
	if c.isStopped() {
		return
	}
	c.publisher.ProofTimeout()
	if err := c.TryProcessQueue(ctx); err != nil {
		c.logger.Error().Err(err).Msg("failed to process queue after proof timeout")
	}
}

// shouldRequeueOnError determines whether to requeue a request based on the error.
func shouldRequeueOnError(err error) bool {
	return errors.Is(err, scpsupervisor.ErrInstanceAlreadyActive) ||
		strings.Contains(err.Error(), "scp instance already active")
}

// Stop stops the sbcpController by marking it as stopped.
func (c *sbcpController) Stop(ctx context.Context) error {
	if !c.markStopped() {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	size, err := c.queue.Size(ctx)
	if err != nil {
		c.logger.Warn().Err(err).Msg("failed to fetch queue size during stop")
	} else {
		c.logger.Info().Int("remaining_requests", size).Msg("stopping sbcp sbcpController")
	}

	return nil
}

func (c *sbcpController) isStopped() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stopped
}

func (c *sbcpController) markStopped() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return false
	}
	c.stopped = true
	return true
}
