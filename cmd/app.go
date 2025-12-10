package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	apisrv "github.com/compose-network/publisher/server/api"
	apimw "github.com/compose-network/publisher/server/api/middleware"
	publishermanager "github.com/compose-network/publisher/x/publisher-manager"
	"github.com/compose-network/publisher/x/superblock"
	"github.com/compose-network/publisher/x/superblock/proofs/collector"
	proofshttp "github.com/compose-network/publisher/x/superblock/proofs/http"
	"github.com/compose-network/publisher/x/superblock/queue"
	"github.com/compose-network/publisher/x/transport"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"

	"github.com/compose-network/publisher/cmd/config"
	"github.com/compose-network/publisher/metrics"
	"github.com/compose-network/publisher/x/auth"
	"github.com/compose-network/publisher/x/consensus"

	sreg "github.com/compose-network/publisher/x/superblock/registry"
	"github.com/compose-network/publisher/x/transport/tcp"
	pb "github.com/compose-network/specs/compose/proto"
)

// App represents the shared publisher application
type App struct {
	cfg       *config.Config
	pmgr      publishermanager.PublisherManager
	log       zerolog.Logger
	tcpServer transport.Server

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
func (a *App) setupL1Config() error {
	// Hydrate a.cfg.L1 before wiring the other components
	// Transition period: prefer explicit --l1.chain-id / config, but default loudly to 560048 if missing
	if a.cfg.L1.ChainID == 0 {
		a.log.Warn().
			Msg("l1.chain_id not provided; DEFAULTING to 560048 (hoodi). This flag will be mandatory soon.")
		a.cfg.L1.ChainID = 560048
	}

	regSvc, err := sreg.NewComposeService(a.cfg.Registry.Path, a.cfg.L1.ChainID, a.log)
	if err != nil {
		return fmt.Errorf("failed to create compose registry service: %w", err)
	}

	// Fill RPCEndpoint from registry if empty
	if strings.TrimSpace(a.cfg.L1.RPCEndpoint) == "" {
		if v := strings.TrimSpace(regSvc.L1PublicRPC()); v != "" {
			a.log.Info().Str("rpc_endpoint", v).Msg("Using L1 RPC endpoint from registry")
			a.cfg.L1.RPCEndpoint = v
		}
	} else {
		if v := strings.TrimSpace(regSvc.L1PublicRPC()); v != "" && v != strings.TrimSpace(a.cfg.L1.RPCEndpoint) {
			a.log.Warn().
				Str("config_rpc_endpoint", a.cfg.L1.RPCEndpoint).
				Str("registry_rpc_endpoint", v).
				Msg("Configured L1 RPC endpoint differs from registry")
		}
	}

	// Fill DisputeGameFactory from registry if empty
	if strings.TrimSpace(a.cfg.L1.DisputeGameFactory) == "" {
		v := strings.TrimSpace(regSvc.PublisherDisputeGameFactory())
		if v != "" {
			a.log.Info().Str("dispute_game_factory", v).Msg("Using DisputeGameFactory from registry")
			a.cfg.L1.DisputeGameFactory = v
		}
	} else {
		v := strings.TrimSpace(regSvc.PublisherDisputeGameFactory())
		if v != "" && !strings.EqualFold(v, strings.TrimSpace(a.cfg.L1.DisputeGameFactory)) {
			a.log.Warn().
				Str("config_dgf", a.cfg.L1.DisputeGameFactory).
				Str("registry_dgf", v).
				Msg("Configured DisputeGameFactory differs from registry")
		}
	}

	return nil
}

func (a *App) setupTransport() (transport.Server, error) {
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
			return nil, fmt.Errorf("failed to initialize auth manager: %w", err)
		}

		for _, seq := range a.cfg.Auth.TrustedSequencers {
			pubKeyBytes, err := hex.DecodeString(seq.PublicKey)
			if err != nil {
				return nil, fmt.Errorf("invalid public key for %s: %w", seq.ID, err)
			}
			if err := authManager.AddTrustedKey(seq.ID, pubKeyBytes); err != nil {
				return nil, fmt.Errorf("failed to add trusted key for %s: %w", seq.ID, err)
			}
			a.log.Info().Str("id", seq.ID).Msg("Added trusted sequencer")
		}

		tcpServer = tcpServer.(*tcp.Server).WithAuth(authManager)
		a.log.Info().
			Str("address", authManager.Address()).
			Str("public_key", authManager.PublicKeyString()).
			Msg("Authentication enabled for shared publisher")
	}

	return tcpServer, nil
}

func (a *App) setupAPIServer(collectorSvc collector.Service) *apisrv.Server {
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

	return s
}

func (a *App) initialize(ctx context.Context) error {
	if err := a.setupL1Config(); err != nil {
		return err

	}

	consensusConfig := consensus.Config{
		NodeID:   fmt.Sprintf("publisher-%d", time.Now().UnixNano()),
		IsLeader: true,
		Timeout:  a.cfg.Consensus.InstanceTimeout,
		Role:     consensus.Leader,
	}
	coordinator := consensus.New(a.log, consensusConfig)
	a.coordinatorShutdownFn = coordinator.Stop

	tcpServer, err := a.setupTransport()
	if err != nil {
		return err
	}

	coordinatorConfig := superblock.DefaultConfig()
	coordinatorConfig.Queue = queue.Config{
		MaxSize:           1000,
		RequestExpiration: 30 * time.Second,
	}
	coordinatorConfig.L1 = a.cfg.L1
	coordinatorConfig.Proofs = a.cfg.Proofs

	collectorSvc := collector.New(ctx, a.log)

	publisherMgr, err := publishermanager.New(publishermanager.Config{
		Context:         ctx,
		Logger:          a.log,
		Broadcaster:     tcpServer,
		InstanceTimeout: a.cfg.Consensus.InstanceTimeout,
		EpochsPerPeriod: a.cfg.Consensus.EpochPerPediods,
	})
	if err != nil {
		return fmt.Errorf("failed to create publisher manager: %w", err)
	}

	a.pmgr = publisherMgr
	a.tcpServer = tcpServer
	a.tcpServer.SetHandler(func(ctx context.Context, from string, msg *pb.Message) error {
		return a.pmgr.HandleMessage(ctx, from, msg)
	})

	a.apiServer = a.setupAPIServer(collectorSvc)

	return nil
}

// Run starts the application and blocks until shutdown.
func (a *App) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	if a.tcpServer != nil {
		if err := a.tcpServer.Start(runCtx); err != nil {
			return fmt.Errorf("failed to start TCP server: %w", err)
		}
	}

	if err := a.pmgr.Start(runCtx); err != nil {
		return fmt.Errorf("failed to start publisher manager: %w", err)
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

	var shutdownErr error
	if err := a.pmgr.Stop(shutdownCtx); err != nil {
		a.log.Error().Err(err).Msg("Publisher manager shutdown error")
		shutdownErr = err
	}
	if a.tcpServer != nil {
		if err := a.tcpServer.Stop(shutdownCtx); err != nil {
			a.log.Error().Err(err).Msg("TCP server shutdown error")
			if shutdownErr == nil {
				shutdownErr = err
			}
		}
	}

	// Run shutdown functions
	for _, fn := range a.shutdownFns {
		if err := fn(); err != nil {
			a.log.Error().Err(err).Msg("Shutdown function error")
		}
	}

	a.log.Info().Msg("Graceful shutdown complete")
	return shutdownErr
}

// handleHealth responds to health check requests.
func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"healthy","timestamp":"%s"}`, time.Now().UTC().Format(time.RFC3339))
}

func (a *App) handleReady(w http.ResponseWriter, r *http.Request) {
}

func (a *App) handleStats(w http.ResponseWriter, r *http.Request) {
}

// GetStats returns application statistics.
func (a *App) GetStats() map[string]interface{} {
	return nil
}

// metricsReporter periodically reports application statistics.
func (a *App) metricsReporter(ctx context.Context) {
}
