package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"

	"github.com/ssvlabs/rollup-shared-publisher/internal/config"
	"github.com/ssvlabs/rollup-shared-publisher/internal/network"
	"github.com/ssvlabs/rollup-shared-publisher/internal/publisher"
	"github.com/ssvlabs/rollup-shared-publisher/pkg/logger"
	"github.com/ssvlabs/rollup-shared-publisher/pkg/metrics"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	var (
		configPath  = flag.String("config", "configs/config.yaml", "Path to configuration file")
		showVersion = flag.Bool("version", false, "Show version information")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("POC Shared Publisher\n")
		fmt.Printf("Version:    %s\n", Version)
		fmt.Printf("Build Time: %s\n", BuildTime)
		fmt.Printf("Git Commit: %s\n", GitCommit)
		fmt.Printf("Go Version: %s\n", runtime.Version())
		os.Exit(0)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(cfg.Log.Level, cfg.Log.Pretty)
	log.Info().
		Str("version", Version).
		Str("build_time", BuildTime).
		Str("go_version", runtime.Version()).
		Msg("Starting POC Shared Publisher")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prometheus.MustRegister(metrics.NewRuntimeCollector())

	serverCfg := network.ServerConfig{
		ListenAddr:     cfg.Server.ListenAddr,
		WriteTimeout:   cfg.Server.WriteTimeout,
		MaxMessageSize: cfg.Server.MaxMessageSize,
		MaxConnections: cfg.Server.MaxConnections,
	}

	server := network.NewServer(serverCfg, log.Logger)

	pub := publisher.New(cfg, server, log.Logger)

	if err := pub.Start(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to start publisher")
		return
	}

	httpServer := startHTTPServer(pub, cfg, log.Logger)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	log.Info().Str("signal", sig.String()).Msg("Received shutdown signal")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	log.Info().Msg("Shutting down HTTP server...")
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("HTTP server shutdown error")
	}

	log.Info().Msg("Stopping publisher...")
	if err := pub.Stop(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Publisher shutdown error")
	}

	log.Info().Msg("Shutdown complete")
}

// startHTTPServer starts the HTTP server for metrics and health
func startHTTPServer(pub *publisher.Publisher, cfg *config.Config, log zerolog.Logger) *http.Server {
	handler := publisher.NewHTTPHandler(pub, log)

	addr := ":8081"
	if cfg.Metrics.Port > 0 {
		addr = fmt.Sprintf(":%d", cfg.Metrics.Port)
	}

	server := &http.Server{
		Addr:         addr,
		Handler:      handler.RegisterRoutes(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Info().Str("addr", addr).Msg("Starting HTTP server")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("HTTP server error")
		}
	}()

	return server
}
