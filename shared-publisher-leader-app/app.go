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

	apisrv "github.com/ssvlabs/rollup-shared-publisher/server/api"
	apimw "github.com/ssvlabs/rollup-shared-publisher/server/api/middleware"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock"
	sbadapter "github.com/ssvlabs/rollup-shared-publisher/x/superblock/adapter"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/batch"
	batchhttp "github.com/ssvlabs/rollup-shared-publisher/x/superblock/batch/http"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs/collector"
	proofshttp "github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs/http"
	proofclient "github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs/prover"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/queue"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/slot"
	"github.com/ssvlabs/rollup-shared-publisher/x/transport"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"

	"github.com/ssvlabs/rollup-shared-publisher/metrics"
	"github.com/ssvlabs/rollup-shared-publisher/shared-publisher-leader-app/config"
	"github.com/ssvlabs/rollup-shared-publisher/x/auth"
	"github.com/ssvlabs/rollup-shared-publisher/x/consensus"
	"github.com/ssvlabs/rollup-shared-publisher/x/publisher"
	"github.com/ssvlabs/rollup-shared-publisher/x/transport/tcp"
)

// App represents the shared publisher application
type App struct {
	cfg       *config.Config
	publisher publisher.Publisher
	log       zerolog.Logger

	// Batch components
	epochTracker  *batch.EpochTracker
	batchManager  *batch.Manager
	batchPipeline *batch.Pipeline

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

// initialize sets up the application components
func (a *App) initialize(ctx context.Context) error {
	coordinator, tcpServer, err := a.initializeTransportAndConsensus()
	if err != nil {
		return err
	}

	collectorSvc, proverClient, err := a.initializeProofServices(ctx)
	if err != nil {
		return err
	}

	slotManager, err := a.initializePublisher(coordinator, tcpServer, collectorSvc, proverClient)
	if err != nil {
		return err
	}

	if err := a.initializeBatchSystem(slotManager, collectorSvc, proverClient); err != nil {
		return err
	}

	if err := a.initializeAPIServer(collectorSvc); err != nil {
		return err
	}

	return nil
}

// initializeTransportAndConsensus sets up consensus coordinator and TCP transport
func (a *App) initializeTransportAndConsensus() (consensus.Coordinator, transport.Server, error) {
	consensusConfig := consensus.Config{
		NodeID:   fmt.Sprintf("publisher-%d", time.Now().UnixNano()),
		IsLeader: true,
		Timeout:  a.cfg.Consensus.Timeout,
		Role:     consensus.Leader,
	}
	coordinator := consensus.New(a.log, consensusConfig)
	a.coordinatorShutdownFn = coordinator.Stop

	transportConfig := transport.Config{
		ListenAddr:     a.cfg.Server.ListenAddr,
		MaxConnections: a.cfg.Server.MaxConnections,
		ReadTimeout:    a.cfg.Server.ReadTimeout,
		WriteTimeout:   a.cfg.Server.WriteTimeout,
		MaxMessageSize: a.cfg.Server.MaxMessageSize,
	}

	tcpServer := tcp.NewServer(transportConfig, a.log)

	if a.cfg.Auth.Enabled {
		authManager, err := auth.NewManagerFromHex(a.cfg.Auth.PrivateKey)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to initialize auth manager: %w", err)
		}

		for _, seq := range a.cfg.Auth.TrustedSequencers {
			pubKeyBytes, err := hex.DecodeString(seq.PublicKey)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid public key for %s: %w", seq.ID, err)
			}
			if err := authManager.AddTrustedKey(seq.ID, pubKeyBytes); err != nil {
				return nil, nil, fmt.Errorf("failed to add trusted key for %s: %w", seq.ID, err)
			}
			a.log.Info().Str("id", seq.ID).Msg("Added trusted sequencer")
		}

		tcpServer = tcpServer.(*tcp.Server).WithAuth(authManager)
		a.log.Info().
			Str("address", authManager.Address()).
			Str("public_key", authManager.PublicKeyString()).
			Msg("Authentication enabled for shared publisher")
	}

	return coordinator, tcpServer, nil
}

// initializeProofServices sets up proof collector and prover client
func (a *App) initializeProofServices(ctx context.Context) (collector.Service, proofs.ProverClient, error) {
	collectorSvc := collector.New(ctx, a.log)

	var proverClient proofs.ProverClient
	if a.cfg.Proofs.Enabled && strings.TrimSpace(a.cfg.Proofs.Prover.BaseURL) != "" {
		pc, err := proofclient.NewHTTPClient(a.cfg.Proofs.Prover.BaseURL, nil, a.log)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create prover client: %w", err)
		}
		proverClient = pc
	}

	return collectorSvc, proverClient, nil
}

// initializePublisher creates the superblock publisher with shared slot manager
func (a *App) initializePublisher(
	coordinator consensus.Coordinator,
	tcpServer transport.Server,
	collectorSvc collector.Service,
	proverClient proofs.ProverClient,
) (*slot.Manager, error) {
	// Slot timing configuration - always use app genesis time
	slotGenesisTime := time.Unix(a.cfg.GenesisTime, 0).UTC()
	slotDuration := 12 * time.Second // Ethereum slot time (12s)

	coordinatorConfig := superblock.DefaultConfig()
	coordinatorConfig.Slot = slot.Config{
		Duration:    slotDuration,
		SealCutover: 0.90,
		GenesisTime: slotGenesisTime,
	}
	coordinatorConfig.Queue = queue.Config{
		MaxSize:           1000,
		RequestExpiration: 30 * time.Second,
	}
	coordinatorConfig.L1 = a.cfg.L1
	coordinatorConfig.Proofs = a.cfg.Proofs

	pub, err := publisher.New(
		a.log,
		publisher.WithTransport(tcpServer),
		publisher.WithConsensus(coordinator),
		publisher.WithTimeout(a.cfg.Consensus.Timeout),
		publisher.WithMetrics(a.cfg.Metrics.Enabled),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create publisher: %w", err)
	}

	sharedSlotManager := slot.NewManager(
		slotGenesisTime,
		coordinatorConfig.Slot.Duration,
		coordinatorConfig.Slot.SealCutover,
	)

	sbPub, err := sbadapter.WrapPublisher(
		pub,
		coordinatorConfig,
		a.log,
		coordinator,
		tcpServer,
		collectorSvc,
		proverClient,
		sharedSlotManager,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create superblock publisher: %w", err)
	}
	a.publisher = sbPub

	return sharedSlotManager, nil
}

// initializeBatchSystem sets up batch synchronization components if enabled
func (a *App) initializeBatchSystem(
	slotManager *slot.Manager,
	collectorSvc collector.Service,
	proverClient proofs.ProverClient,
) error {
	if !a.cfg.Batch.Enabled {
		return nil
	}

	a.log.Info().
		Uint32("chain_id", a.cfg.Batch.ChainID).
		Int64("app_genesis_time", a.cfg.GenesisTime).
		Int64("ethereum_genesis", a.cfg.Batch.EthereumGenesis).
		Msg("Initializing batch synchronization")

	epochTrackerCfg := a.cfg.Batch.GetEpochTrackerConfig()
	epochTracker, err := batch.NewEpochTracker(epochTrackerCfg, a.log)
	if err != nil {
		return fmt.Errorf("failed to create epoch tracker: %w", err)
	}
	a.epochTracker = epochTracker

	managerCfg := a.cfg.Batch.GetManagerConfig()
	batchManager, err := batch.NewManager(managerCfg, slotManager, epochTracker, a.log)
	if err != nil {
		return fmt.Errorf("failed to create batch manager: %w", err)
	}
	a.batchManager = batchManager

	pipelineCfg := a.cfg.Batch.GetPipelineConfig()
	batchPipeline, err := batch.NewPipeline(pipelineCfg, batchManager, collectorSvc, proverClient, a.log)
	if err != nil {
		return fmt.Errorf("failed to create batch pipeline: %w", err)
	}
	a.batchPipeline = batchPipeline

	a.log.Info().Msg("Batch synchronization initialized successfully")
	return nil
}

// initializeAPIServer sets up the HTTP API server with all endpoints
func (a *App) initializeAPIServer(collectorSvc collector.Service) error {
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

	// Batch API
	if a.cfg.Batch.Enabled {
		batchHandler := batchhttp.NewHandler(a.epochTracker, a.batchManager, a.batchPipeline, a.log)
		batchHandler.RegisterMux(s.Router)
	}

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

	if a.cfg.Batch.Enabled {
		if a.epochTracker != nil {
			go a.epochTracker.Start(runCtx)
			a.log.Info().Msg("Epoch tracker started")
		}

		if a.batchManager != nil {
			go a.batchManager.Start(runCtx)
			a.log.Info().Msg("Batch manager started")
		}

		if a.batchPipeline != nil {
			go a.batchPipeline.Start(runCtx)
			a.log.Info().Msg("Batch pipeline started")
		}
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

	// Stop batch components
	if a.batchPipeline != nil {
		if err := a.batchPipeline.Stop(shutdownCtx); err != nil {
			a.log.Error().Err(err).Msg("Batch pipeline shutdown error")
		}
	}

	if a.batchManager != nil {
		if err := a.batchManager.Stop(shutdownCtx); err != nil {
			a.log.Error().Err(err).Msg("Batch manager shutdown error")
		}
	}

	if a.epochTracker != nil {
		if err := a.epochTracker.Stop(shutdownCtx); err != nil {
			a.log.Error().Err(err).Msg("Epoch tracker shutdown error")
		}
	}

	// Shutdown consensus coordinator
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
