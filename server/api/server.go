package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
)

type Server struct {
	cfg Config
	log zerolog.Logger

	Router *mux.Router
	http   *http.Server
	chain  []func(http.Handler) http.Handler

	mtx      sync.Mutex
	listener net.Listener
}

func NewServer(cfg Config, log zerolog.Logger) *Server {
	r := mux.NewRouter()
	s := &Server{
		cfg:    cfg,
		log:    log.With().Str("component", "http-api").Logger(),
		Router: r,
		chain:  make([]func(http.Handler) http.Handler, 0),
	}

	s.http = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           r,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}

	return s
}

// Use appends middleware to the chain and rebuilds the handler
func (s *Server) Use(mw func(http.Handler) http.Handler) {
	s.chain = append(s.chain, mw)
	s.http.Handler = s.buildHandler()
}

// buildHandler constructs the middleware chain
func (s *Server) buildHandler() http.Handler {
	h := http.Handler(s.Router)
	for i := len(s.chain) - 1; i >= 0; i-- {
		h = s.chain[i](h)
	}
	return h
}

// Start runs the HTTP server with a dedicated listener and graceful shutdown
func (s *Server) Start(ctx context.Context) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.cfg.ListenAddr)
	if err != nil {
		return err
	}

	s.mtx.Lock()
	s.listener = ln
	s.mtx.Unlock()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutdownCtx)
	}()

	s.log.Info().Str("addr", s.cfg.ListenAddr).Msg("HTTP API server starting")
	err = s.http.Serve(ln)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	s.log.Info().Msg("HTTP API server stopped")
	return nil
}

// EnableCORS enables permissive CORS. Use sparingly in prod.
func (s *Server) EnableCORS() {
	s.Use(func(next http.Handler) http.Handler {
		return handlers.CORS(
			handlers.AllowedHeaders([]string{"Content-Type", "X-Request-ID"}),
			handlers.AllowedOrigins([]string{"*"}),
			handlers.AllowedMethods([]string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}),
		)(next)
	})
}
