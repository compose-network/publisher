package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	apisrv "github.com/compose-network/publisher/server/api"
	apimw "github.com/compose-network/publisher/server/api/middleware"
	"github.com/compose-network/publisher/x/superblock"
	sbadapter "github.com/compose-network/publisher/x/superblock/adapter"
	"github.com/compose-network/publisher/x/superblock/proofs"
	"github.com/compose-network/publisher/x/superblock/proofs/collector"
	proofshttp "github.com/compose-network/publisher/x/superblock/proofs/http"
	proofclient "github.com/compose-network/publisher/x/superblock/proofs/prover"
	"github.com/compose-network/publisher/x/superblock/queue"
	"github.com/compose-network/publisher/x/superblock/slot"
	"github.com/compose-network/publisher/x/transport"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"

	"github.com/compose-network/publisher/metrics"
	"github.com/compose-network/publisher/publisher-leader-app/config"
	"github.com/compose-network/publisher/x/auth"
	"github.com/compose-network/publisher/x/consensus"
	"github.com/compose-network/publisher/x/publisher"
	"github.com/compose-network/publisher/x/transport/tcp"
)

// App represents the shared publisher application
type App struct {
	cfg       *config.Config
	publisher publisher.Publisher
	log       zerolog.Logger

	// API server (HTTP)
	apiServer *apisrv.Server

	// Shutdown management
	shutdownFns           []func() error
	coordinatorShutdownFn func(ctx context.Context) error

	cancel context.CancelFunc
}

// NewApp creates a new application instance
func NewApp(ctx context.Context, cfg *config.Config, log zerolog.Logger) (*App, error) {
	app := &App{
		cfg:         cfg,
		log:         log.With().Str("component", "app").Logger(),
		shutdownFns: make([]func() error, 0),
	}

	if err := app.initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize app: %w", err)
	}

	return app, nil
}

// initialize sets up the application components such as consensus, transport, authentication, metrics, and publisher.
func (a *App) initialize(ctx context.Context) error {
	consensusConfig := consensus.Config{
		NodeID:   fmt.Sprintf("publisher-%d", time.Now().UnixNano()),
		IsLeader: true,
		Timeout:  a.cfg.Consensus.Timeout,
		Role:     consensus.Leader,
	}
	coordinator := consensus.New(a.log, consensusConfig)
	a.coordinatorShutdownFn = coordinator.Stop

	transportConfig := transport.Config{}
	transportConfig.ListenAddr = a.cfg.Server.ListenAddr
	transportConfig.MaxConnections = a.cfg.Server.MaxConnections
	transportConfig.ReadTimeout = a.cfg.Server.ReadTimeout
	transportConfig.WriteTimeout = a.cfg.Server.WriteTimeout
	transportConfig.MaxMessageSize = a.cfg.Server.MaxMessageSize

	tcpServer := tcp.NewServer(transportConfig, a.log)

	if a.cfg.Auth.Enabled {
		authManager, err := auth.NewManagerFromHex(a.cfg.Auth.PrivateKey)
		if err != nil {
			return fmt.Errorf("failed to initialize auth manager: %w", err)
		}

		for _, seq := range a.cfg.Auth.TrustedSequencers {
			pubKeyBytes, err := hex.DecodeString(seq.PublicKey)
			if err != nil {
				return fmt.Errorf("invalid public key for %s: %w", seq.ID, err)
			}
			if err := authManager.AddTrustedKey(seq.ID, pubKeyBytes); err != nil {
				return fmt.Errorf("failed to add trusted key for %s: %w", seq.ID, err)
			}
			a.log.Info().Str("id", seq.ID).Msg("Added trusted sequencer")
		}

		tcpServer = tcpServer.(*tcp.Server).WithAuth(authManager)
		a.log.Info().
			Str("address", authManager.Address()).
			Str("public_key", authManager.PublicKeyString()).
			Msg("Authentication enabled for shared publisher")
	}

	coordinatorConfig := superblock.DefaultConfig()
	coordinatorConfig.Slot = slot.Config{
		Duration:    12 * time.Second,
		SealCutover: 2.0 / 3.0,
		GenesisTime: time.Now(),
	}
	coordinatorConfig.Queue = queue.Config{
		MaxSize:           1000,
		RequestExpiration: 30 * time.Second,
	}
	coordinatorConfig.L1 = a.cfg.L1
	coordinatorConfig.Proofs = a.cfg.Proofs

	collectorSvc := collector.New(ctx, a.log)

	var proverClient proofs.ProverClient
	if a.cfg.Proofs.Enabled && strings.TrimSpace(a.cfg.Proofs.Prover.BaseURL) != "" {
		pc, err := proofclient.NewHTTPClient(a.cfg.Proofs.Prover.BaseURL, nil, a.log)
		if err != nil {
			return fmt.Errorf("failed to create prover client: %w", err)
		}
		proverClient = pc
	}

	pub, err := publisher.New(
		a.log,
		publisher.WithTransport(tcpServer),
		publisher.WithConsensus(coordinator),
		publisher.WithTimeout(a.cfg.Consensus.Timeout),
		publisher.WithMetrics(a.cfg.Metrics.Enabled),
	)
	if err != nil {
		return fmt.Errorf("failed to create publisher: %w", err)
	}

	sbPub, err := sbadapter.WrapPublisher(
		pub,
		coordinatorConfig,
		a.log,
		coordinator,
		tcpServer,
		collectorSvc,
		proverClient,
	)
	if err != nil {
		return fmt.Errorf("failed to create superblock publisher: %w", err)
	}
	a.publisher = sbPub

	// API server (shared HTTP surface)
	apiCfg := apisrv.Config{
		ListenAddr:        a.cfg.API.ListenAddr,
		ReadHeaderTimeout: a.cfg.API.ReadHeaderTimeout,
		ReadTimeout:       a.cfg.API.ReadTimeout,
		WriteTimeout:      a.cfg.API.WriteTimeout,
		IdleTimeout:       a.cfg.API.IdleTimeout,
		MaxHeaderBytes:    a.cfg.API.MaxHeaderBytes,
	}
	s := apisrv.NewServer(apiCfg, a.log)
	s.Use(apimw.Recover(a.log))
	s.Use(apimw.RequestID())
	s.Use(apimw.Logger(a.log))

	// Health/readiness/stats
	s.Router.HandleFunc("/health", a.handleHealth).Methods(http.MethodGet)
	s.Router.HandleFunc("/ready", a.handleReady).Methods(http.MethodGet)
	s.Router.HandleFunc("/stats", a.handleStats).Methods(http.MethodGet)

	// Metrics
	if a.cfg.Metrics.Enabled {
		s.Router.Handle("/metrics", promhttp.HandlerFor(metrics.GetRegistry(), promhttp.HandlerOpts{})).
			Methods(http.MethodGet)
	}

	// Proofs API
	proofHandler := proofshttp.NewHandler(collectorSvc, a.log)
	proofHandler.RegisterMux(s.Router)

	a.apiServer = s

	return nil
}

// Run starts the application and blocks until shutdown.
func (a *App) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	if err := a.publisher.Start(runCtx); err != nil {
		return fmt.Errorf("failed to start publisher: %w", err)
	}

	go a.metricsReporter(runCtx)

	// Start API server
	if a.apiServer != nil {
		go func() {
			if err := a.apiServer.Start(runCtx); err != nil {
				a.log.Error().Err(err).Msg("API server error")
			}
		}()
	}

	return a.runWithGracefulShutdown(runCtx)
}

// runWithGracefulShutdown handles shutdown signals.
func (a *App) runWithGracefulShutdown(ctx context.Context) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	a.log.Info().Msg("Rollup shared publisher started successfully")

	select {
	case <-ctx.Done():
		a.log.Info().Msg("Context canceled, initiating shutdown")
	case sig := <-sigCh:
		a.log.Info().Str("signal", sig.String()).Msg("Received shutdown signal")
	}

	if a.cancel != nil {
		a.cancel()
	}

	return a.shutdown()
}

// shutdown gracefully shuts down the application by stopping
// the HTTP server, publisher, and executing shutdown functions.
func (a *App) shutdown() error {
	a.log.Info().Msg("Initiating graceful shutdown")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown consensus coordinator first
	if a.coordinatorShutdownFn != nil {
		if err := a.coordinatorShutdownFn(shutdownCtx); err != nil {
			a.log.Error().Err(err).Msg("Consensus coordinator shutdown error")
		}
	}

	// Shutdown publisher
	if err := a.publisher.Stop(shutdownCtx); err != nil {
		a.log.Error().Err(err).Msg("Publisher shutdown error")
		return err
	}

	// Run shutdown functions
	for _, fn := range a.shutdownFns {
		if err := fn(); err != nil {
			a.log.Error().Err(err).Msg("Shutdown function error")
		}
	}

	a.log.Info().Msg("Graceful shutdown complete")
	return nil
}

// handleHealth responds to health check requests.
func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"healthy","timestamp":"%s"}`, time.Now().UTC().Format(time.RFC3339))
}

func (a *App) handleReady(w http.ResponseWriter, r *http.Request) {
	stats := a.publisher.GetStats()
	connections := stats["active_connections"].(int)

	status := "ready"
	code := http.StatusOK

	if connections == 0 {
		status = "no_connections"
		code = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	fmt.Fprintf(w, `{"status":"%s","connections":%d}`, status, connections)
}

func (a *App) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := a.publisher.GetStats()
	stats["app_version"] = Version
	stats["app_build_time"] = BuildTime
	stats["app_git_commit"] = GitCommit

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// GetStats returns application statistics.
func (a *App) GetStats() map[string]interface{} {
	stats := a.publisher.GetStats()
	stats["app_version"] = Version
	stats["app_build_time"] = BuildTime
	stats["app_git_commit"] = GitCommit
	return stats
}

// metricsReporter periodically reports application statistics.
func (a *App) metricsReporter(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := a.GetStats()

			a.log.Info().
				Str("mode", "leader").
				Int("active_connections", stats["active_connections"].(int)).
				Uint64("messages_processed", stats["messages_processed"].(uint64)).
				Uint64("broadcasts_sent", stats["broadcasts_sent"].(uint64)).
				Int("chains_count", stats["chains_count"].(int)).
				Int("active_2pc_transactions", stats["active_2pc_transactions"].(int)).
				Float64("uptime_seconds", stats["uptime_seconds"].(float64)).
				Msg("Shared Publisher statistics")
		}
	}
}
