package publisher

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ssvlabs/rollup-shared-publisher/internal/consensus"

	"github.com/rs/zerolog"

	"github.com/ssvlabs/rollup-shared-publisher/internal/config"
	"github.com/ssvlabs/rollup-shared-publisher/internal/network"
	pb "github.com/ssvlabs/rollup-shared-publisher/internal/proto"
	"github.com/ssvlabs/rollup-shared-publisher/pkg/metrics"
)

// Publisher orchestrates the shared publisher functionality.
type Publisher struct {
	cfg    *config.Config
	server network.Server
	log    zerolog.Logger

	mu        sync.RWMutex
	chains    map[string]bool
	started   time.Time
	startTime time.Time

	msgCount     atomic.Uint64
	broadcastCnt atomic.Uint64
	metrics      *Metrics

	activeTxs   sync.Map
	nextXTID    atomic.Uint32
	coordinator *consensus.Coordinator
}

// New creates a new publisher instance.
func New(cfg *config.Config, server network.Server, log zerolog.Logger) *Publisher {
	p := &Publisher{
		cfg:         cfg,
		server:      server,
		log:         log.With().Str("component", "publisher").Logger(),
		chains:      make(map[string]bool),
		coordinator: consensus.NewCoordinator(log),
		metrics:     NewMetrics(),
		startTime:   time.Now(),
	}

	p.coordinator.SetBroadcastCallback(p.broadcastDecision)

	return p
}

// Start starts the publisher.
func (p *Publisher) Start(ctx context.Context) error {
	p.log.Info().Msg("Starting publisher")

	p.started = time.Now()

	p.server.SetHandler(p.handleMessage)

	if err := p.server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	go metrics.StartPeriodicCollection(ctx, time.Second, time.Now())
	go p.metricsReporter(ctx)

	p.log.Info().
		Str("version", "0.1.0").
		Str("address", p.cfg.Server.ListenAddr).
		Msg("Publisher started successfully")

	return nil
}

// Stop stops the publisher.
func (p *Publisher) Stop(ctx context.Context) error {
	p.log.Info().Msg("Stopping publisher")

	if err := p.server.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop server: %w", err)
	}

	p.log.Info().
		Uint64("messages_processed", p.msgCount.Load()).
		Uint64("broadcasts_sent", p.broadcastCnt.Load()).
		Msg("Publisher stopped")

	return nil
}

func (p *Publisher) handleMessage(ctx context.Context, from string, msg *pb.Message) error {
	p.msgCount.Add(1)

	var err error

	switch payload := msg.Payload.(type) {
	case *pb.Message_XtRequest:
		err = p.handleXTRequest(ctx, from, msg, payload.XtRequest)
	case *pb.Message_Vote:
		err = p.handleVote(from, payload.Vote)
	case *pb.Message_Block:
		err = p.handleBlock(ctx, from, payload.Block)
	default:
		p.metrics.RecordError("unknown_message_type", "handle_message")
		err = fmt.Errorf("unknown message type: %T", payload)
	}

	return err
}

// metricsReporter periodically reports internal metrics.
func (p *Publisher) metricsReporter(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			connections := p.server.GetConnections()

			p.mu.RLock()
			chainCount := len(p.chains)
			p.mu.RUnlock()

			p.log.Info().
				Int("active_connections", len(connections)).
				Uint64("messages_processed", p.msgCount.Load()).
				Uint64("broadcasts_sent", p.broadcastCnt.Load()).
				Int("unique_chains", chainCount).
				Dur("uptime", time.Since(p.started)).
				Msg("Publisher statistics")
		}
	}
}

// GetStats returns current statistics.
func (p *Publisher) GetStats() map[string]interface{} {
	connections := p.server.GetConnections()

	p.mu.RLock()
	chains := make([]string, 0, len(p.chains))
	for chain := range p.chains {
		chains = append(chains, chain)
	}
	p.mu.RUnlock()

	activeTxs := p.coordinator.GetActiveTransactions()

	return map[string]interface{}{
		"uptime_seconds":          time.Since(p.started).Seconds(),
		"active_connections":      len(connections),
		"messages_processed":      p.msgCount.Load(),
		"broadcasts_sent":         p.broadcastCnt.Load(),
		"unique_chains":           chains,
		"chains_count":            len(chains),
		"active_2pc_transactions": len(activeTxs),
		"active_2pc_ids":          activeTxs,
	}
}
