package publisher

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	pb "github.com/compose-network/publisher/proto/rollup/v1"
	"github.com/compose-network/publisher/x/consensus"
	"github.com/compose-network/publisher/x/transport"
)

// publisher is the central coordinator (LEADER)
type publisher struct {
	transport transport.Transport
	consensus consensus.Coordinator
	log       zerolog.Logger
	router    MessageRouter

	mu      sync.RWMutex
	chains  map[string]bool
	started time.Time

	msgCount     atomic.Uint64
	broadcastCnt atomic.Uint64
	metrics      *Metrics
	activeTxs    sync.Map
}

// New creates a new publisher instance
func New(log zerolog.Logger, opts ...Option) (Publisher, error) {
	config := &Config{
		MetricsEnabled: true,
	}

	for _, opt := range opts {
		opt(config)
	}

	if config.Transport == nil {
		return nil, fmt.Errorf("transport is required")
	}

	if config.Consensus == nil {
		return nil, fmt.Errorf("consensus coordinator is required")
	}

	router := NewMessageRouter()

	p := &publisher{
		transport: config.Transport,
		consensus: config.Consensus,
		log:       log.With().Str("component", "publisher").Logger(),
		router:    router,
		chains:    make(map[string]bool),
		metrics:   NewMetrics(),
	}

	// Register default message handlers
	router.Register(XTRequestType, p.handleXTRequest)
	router.Register(VoteType, p.handleVote)
	router.Register(BlockType, p.handleBlock)

	config.Consensus.SetDecisionCallback(p.broadcastDecision)
	config.Transport.SetHandler(p.HandleMessage)

	return p, nil
}

// Start starts the publisher
func (p *publisher) Start(ctx context.Context) error {
	p.log.Info().Msg("Starting shared publisher")
	p.started = time.Now()

	if err := p.transport.Start(ctx); err != nil {
		return fmt.Errorf("failed to start transport: %w", err)
	}

	p.log.Info().
		Str("version", "1.0.0").
		Msg("Shared publisher started successfully")

	return nil
}

// Stop stops the publisher
func (p *publisher) Stop(ctx context.Context) error {
	p.log.Info().Msg("Stopping shared publisher")

	if err := p.transport.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop transport: %w", err)
	}

	p.log.Info().
		Uint64("messages_processed", p.msgCount.Load()).
		Uint64("broadcasts_sent", p.broadcastCnt.Load()).
		Msg("Shared publisher stopped")

	return nil
}

// broadcastDecision - Publisher BROADCASTS decisions to sequencers
func (p *publisher) broadcastDecision(ctx context.Context, xtID *pb.XtID, decision bool) error {
	decided := &pb.Decided{
		XtId:     xtID,
		Decision: decision,
	}

	msg := &pb.Message{
		SenderId: "publisher",
		Payload: &pb.Message_Decided{
			Decided: decided,
		},
	}

	xtIDStr := xtID.Hex()
	p.log.Info().
		Str("xt_id", xtIDStr).
		Bool("decision", decision).
		Msg("Broadcasting 2PC decision")

	err := p.transport.Broadcast(ctx, msg, "")
	if err != nil {
		p.log.Error().
			Err(err).
			Str("xt_id", xtIDStr).
			Bool("decision", decision).
			Msg("Failed to broadcast 2PC decision")
	} else {
		p.broadcastCnt.Add(1)
	}

	if !decision {
		p.activeTxs.Delete(xtIDStr)
	}

	return err
}

// MessageRouter returns the message router for external handler registration
func (p *publisher) MessageRouter() MessageRouter {
	return p.router
}

// GetStats returns current statistics
func (p *publisher) GetStats() map[string]interface{} {
	connections := p.transport.GetConnections()

	p.mu.RLock()
	chains := make([]string, 0, len(p.chains))
	for chain := range p.chains {
		chains = append(chains, chain)
	}
	p.mu.RUnlock()

	activeTxs := p.consensus.GetActiveTransactions()

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
