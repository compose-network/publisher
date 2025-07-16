package publisher

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

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

	// State
	mu      sync.RWMutex
	chains  map[string]bool // Track unique chains
	started time.Time

	// Metrics
	msgCount     atomic.Uint64
	broadcastCnt atomic.Uint64
}

// New creates a new publisher instance.
func New(cfg *config.Config, server network.Server, log zerolog.Logger) *Publisher {
	return &Publisher{
		cfg:    cfg,
		server: server,
		log:    log.With().Str("component", "publisher").Logger(),
		chains: make(map[string]bool),
	}
}

// Start starts the publisher.
func (p *Publisher) Start(ctx context.Context) error {
	p.log.Info().Msg("Starting publisher")

	p.started = time.Now()

	p.server.SetHandler(p.handleMessage)

	if err := p.server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	go metrics.StartUptimeCollector(ctx)
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
	start := time.Now()
	p.msgCount.Add(1)

	var msgType string
	var err error

	switch payload := msg.Payload.(type) {
	case *pb.Message_XtRequest:
		msgType = "xt_request"
		err = p.handleXTRequest(ctx, from, msg, payload.XtRequest)
	default:
		msgType = "unknown"
		metrics.RecordError("unknown_message_type", "handle_message")
		err = fmt.Errorf("unknown message type: %T", payload)
	}

	metrics.MessageProcessingDuration.WithLabelValues(msgType).Observe(time.Since(start).Seconds())
	return err
}

// handleXTRequest handles cross-chain transaction requests.
func (p *Publisher) handleXTRequest(ctx context.Context, from string, msg *pb.Message, req *pb.XTRequest) error {
	log := p.log.With().
		Str("from", from).
		Str("sender_id", msg.SenderId).
		Int("tx_count", len(req.Transactions)).
		Logger()

	log.Info().Msg("Received xT request")

	// Record metrics
	metrics.CrossChainTransactionsTotal.Inc()
	metrics.TransactionBatchSize.Observe(float64(len(req.Transactions)))

	// Track chains
	p.mu.Lock()
	for _, tx := range req.Transactions {
		chainID := fmt.Sprintf("0x%x", tx.ChainId)
		if !p.chains[chainID] {
			p.chains[chainID] = true
			metrics.UniqueChains.WithLabelValues(chainID).Set(1)
		}
		metrics.TransactionsProcessed.WithLabelValues(chainID).Inc()
	}
	p.mu.Unlock()

	for i, tx := range req.Transactions {
		log.Debug().
			Int("index", i).
			Str("chain_id", fmt.Sprintf("0x%x", tx.ChainId)).
			Int("tx_data_count", len(tx.Transaction)).
			Msg("Transaction details")
	}

	// Broadcast to all other connections
	broadcastStart := time.Now()

	connections := p.server.GetConnections()
	recipientCount := len(connections) - 1 // Exclude sender

	if recipientCount > 0 {
		metrics.BroadcastRecipients.Observe(float64(recipientCount))

		if err := p.server.Broadcast(ctx, msg, from); err != nil {
			log.Error().Err(err).Msg("Failed to broadcast xT request")
			metrics.RecordError("broadcast_failed", "xt_request")
			return err
		}

		p.broadcastCnt.Add(1)
		metrics.BroadcastsTotal.Inc()
		metrics.BroadcastDuration.Observe(time.Since(broadcastStart).Seconds())

		log.Info().
			Int("recipients", recipientCount).
			Dur("duration", time.Since(broadcastStart)).
			Msg("Successfully broadcast xT request")
	} else {
		log.Warn().Msg("No other connections to broadcast to")
	}

	return nil
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

			metrics.ConnectionsActive.Set(float64(len(connections)))
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

	return map[string]interface{}{
		"uptime_seconds":     time.Since(p.started).Seconds(),
		"active_connections": len(connections),
		"messages_processed": p.msgCount.Load(),
		"broadcasts_sent":    p.broadcastCnt.Load(),
		"unique_chains":      chains,
		"chains_count":       len(chains),
	}
}
