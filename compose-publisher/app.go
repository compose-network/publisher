package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/compose-network/compose-sdk/consensus"
	"github.com/compose-network/compose-sdk/transport/quic"
	"github.com/compose-network/specs/compose"
	"github.com/rs/zerolog"

	apisrv "github.com/compose-network/publisher/server/api"
	apimw "github.com/compose-network/publisher/server/api/middleware"

	"github.com/compose-network/publisher/compose-publisher/config"
	"github.com/compose-network/publisher/compose-publisher/sidecar"
)

const periodDuration = 12 * time.Second

// App represents the shared publisher application
type App struct {
	cfg         *config.Config
	coordinator *sidecar.Coordinator
	log         zerolog.Logger

	apiServer   *apisrv.Server
	shutdownFns []func() error
	cancel      context.CancelFunc

	periodID      atomic.Uint64
	superblockNum atomic.Uint64
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

// initialize sets up the application components: QUIC server, consensus, sidecar coordinator, and HTTP API.
func (a *App) initialize(ctx context.Context) error {
	quicCfg := quic.DefaultServerConfig()
	quicCfg.ListenAddr = a.cfg.Server.ListenAddr

	quicSrv := quic.NewServer(quicCfg, a.log)

	consCfg := consensus.DefaultConfig()
	consCfg.NodeID = "publisher"
	consCfg.IsLeader = true
	consCfg.Timeout = a.cfg.Consensus.Timeout

	cons := consensus.New(consCfg, a.log)

	coord := sidecar.NewCoordinator(quicSrv, cons, a.log)
	a.coordinator = coord

	// HTTP API server
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

	s.Router.HandleFunc("/health", a.handleHealth).Methods(http.MethodGet)
	s.Router.HandleFunc("/ready", a.handleReady).Methods(http.MethodGet)
	s.Router.HandleFunc("/stats", a.handleStats).Methods(http.MethodGet)

	a.apiServer = s

	return nil
}

// Run starts the application and blocks until shutdown.
func (a *App) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	if err := a.coordinator.Start(runCtx); err != nil {
		return fmt.Errorf("failed to start coordinator: %w", err)
	}

	go a.periodLoop(runCtx)
	go a.metricsReporter(runCtx)

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

// shutdown gracefully shuts down the application.
func (a *App) shutdown() error {
	a.log.Info().Msg("Initiating graceful shutdown")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := a.coordinator.Stop(shutdownCtx); err != nil {
		a.log.Error().Err(err).Msg("Coordinator shutdown error")
		return err
	}

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
	stats := a.coordinator.GetStats()
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
	stats := a.coordinator.GetStats()
	stats["app_version"] = Version
	stats["app_build_time"] = BuildTime
	stats["app_git_commit"] = GitCommit

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// GetStats returns application statistics.
func (a *App) GetStats() map[string]interface{} {
	stats := a.coordinator.GetStats()
	stats["app_version"] = Version
	stats["app_build_time"] = BuildTime
	stats["app_git_commit"] = GitCommit
	return stats
}

// periodLoop broadcasts StartPeriod to all connected sidecars on a 12-second cycle
// aligned with Ethereum slot timing. Each period increments the superblock number.
func (a *App) periodLoop(ctx context.Context) {
	ticker := time.NewTicker(periodDuration)
	defer ticker.Stop()

	// Broadcast the first period immediately so sidecars become initialized.
	a.broadcastPeriod(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.broadcastPeriod(ctx)
		}
	}
}

func (a *App) broadcastPeriod(ctx context.Context) {
	pid := a.periodID.Add(1)
	sbNum := a.superblockNum.Add(1)

	if err := a.coordinator.StartPeriod(ctx, compose.PeriodID(pid), compose.SuperblockNumber(sbNum)); err != nil {
		a.log.Error().Err(err).Uint64("period_id", pid).Msg("Failed to broadcast StartPeriod")
	}
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
