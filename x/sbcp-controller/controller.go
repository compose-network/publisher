package sbcpcontroller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	scpsupervisor "github.com/compose-network/publisher/x/scp-instance-supervisor"
	"github.com/compose-network/publisher/x/superblock/queue"
	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/compose-network/specs/compose/sbcp"
	"github.com/rs/zerolog"
)

var (
	ErrNilRequest = errors.New("sbcp-controller: nil xt request")
)

// controller implements the Controller interface
type controller struct {
	logger    zerolog.Logger
	publisher sbcp.Publisher
	queue     queue.XTRequestQueue
	starter   InstanceStarter
	now       func() time.Time
}

// New constructs a Controller using the provided config.
func New(cfg Config) (Controller, error) {
	if err := cfg.apply(); err != nil {
		return nil, err
	}

	return &controller{
		logger:    cfg.Logger,
		publisher: cfg.Publisher,
		queue:     cfg.Queue,
		starter:   cfg.InstanceStarter,
		now:       cfg.Now,
	}, nil
}

func (c *controller) SetInstanceStarter(starter InstanceStarter) {
	c.starter = starter
}

// OnNewPeriod should be called whenever a new SBCP period starts.
func (c *controller) OnNewPeriod(ctx context.Context) error {
	if err := c.publisher.StartPeriod(); err != nil {
		c.logger.Warn().Err(err).Msg("failed to start period")
		return fmt.Errorf("sbcp start period: %w", err)
	}
	return c.TryProcessQueue(ctx)
}

// EnqueueXTRequest adds an XT request to the processing queue.
func (c *controller) EnqueueXTRequest(ctx context.Context, req *pb.XTRequest, from string) error {
	if req == nil {
		return ErrNilRequest
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
func (c *controller) TryProcessQueue(ctx context.Context) error {

	// Remove any expired requests first
	if cfg := c.queue.Config(); cfg.RequestExpiration > 0 {
		if _, err := c.queue.RemoveExpired(ctx); err != nil {
			return fmt.Errorf("remove expired requests: %w", err)
		}
	}

	for {
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
func (c *controller) NotifyInstanceDecided(ctx context.Context, instance compose.Instance) error {
	if err := c.publisher.DecideInstance(instance); err != nil {
		c.logger.Error().Err(err).Str("instance_id", instance.ID.String()).Msg("SBCP failed to finalize instance")
		return fmt.Errorf("decide instance: %w", err)
	}
	return c.TryProcessQueue(ctx)
}

// AdvanceSettledState advances the settled superblock state.
func (c *controller) AdvanceSettledState(superblockNumber compose.SuperblockNumber, superblockHash compose.SuperBlockHash) error {
	if err := c.publisher.AdvanceSettledState(superblockNumber, superblockHash); err != nil {
		c.logger.Error().Err(err).
			Uint64("superblock_number", uint64(superblockNumber)).
			Msg("failed to advance settled state")
		return fmt.Errorf("advance settled state: %w", err)
	}
	return nil
}

// ProofTimeout notifies the controller of a proof timeout event.
func (c *controller) ProofTimeout(ctx context.Context) {
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
