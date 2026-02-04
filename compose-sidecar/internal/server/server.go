// Package server implements the HTTP API for builder communication.
package server

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/compose-network/compose-sdk/peer"
	"github.com/compose-network/compose-sdk/protocol"
	"github.com/compose-network/compose-sidecar/internal/coordinator"
	"github.com/compose-network/specs/compose/proto"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/rs/zerolog"
	goproto "google.golang.org/protobuf/proto"
)

// Server handles HTTP requests from op-rbuilder.
type Server struct {
	httpServer  *http.Server
	router      chi.Router
	coordinator coordinator.Coordinator
	log         zerolog.Logger
}

// Config holds server configuration.
type Config struct {
	ListenAddr   string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// NewServer creates a new HTTP server.
func NewServer(cfg Config, coord coordinator.Coordinator, log zerolog.Logger) *Server {
	s := &Server{
		coordinator: coord,
		log:         log.With().Str("component", "server").Logger(),
	}

	r := chi.NewRouter()

	// Middleware
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(s.loggingMiddleware)
	r.Use(middleware.Recoverer)

	// Routes
	r.Post("/transactions", s.handleTransactions)
	r.Post("/mailbox", s.handleMailbox)
	r.Post("/xt", s.handleXTSubmit)
	r.Get("/xt/{instanceID}", s.handleXTStatus)
	r.Post("/xt/forward", s.handleXTForward)
	r.Post("/xt/vote", s.handleXTVote)
	r.Get("/health", s.handleHealth)
	r.Head("/health", s.handleHealth)
	r.Get("/ready", s.handleReady)
	r.Head("/ready", s.handleReady)

	s.router = r
	s.httpServer = &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      r,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	return s
}

// Start starts the HTTP server.
func (s *Server) Start(ctx context.Context) error {
	s.log.Info().Str("addr", s.httpServer.Addr).Msg("Starting HTTP server")

	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("server failed to start: %w", err)
	case <-time.After(100 * time.Millisecond):
		s.log.Info().Msg("HTTP server started")
		return nil
	}
}

// Stop stops the HTTP server gracefully.
func (s *Server) Stop(ctx context.Context) error {
	s.log.Info().Msg("Stopping HTTP server")
	return s.httpServer.Shutdown(ctx)
}

// handleTransactions handles POST /transactions from op-rbuilder.
func (s *Server) handleTransactions(w http.ResponseWriter, r *http.Request) {
	var req protocol.BuilderPollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	if req.ChainID == 0 {
		s.writeError(w, http.StatusBadRequest, "chain_id is required", nil)
		return
	}

	resp, err := s.coordinator.HandleBuilderPoll(r.Context(), &req)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to process poll", err)
		return
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// handleHealth handles GET /health.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReady handles GET /ready.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// TODO: Add readiness checks (publisher connection, etc.)
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.log.Error().Err(err).Msg("Failed to encode response")
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string, err error) {
	s.log.Error().Err(err).Str("message", message).Int("status", status).Msg("Request error")

	resp := map[string]string{"error": message}
	if err != nil {
		resp["details"] = err.Error()
	}
	s.writeJSON(w, status, resp)
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		defer func() {
			s.log.Debug().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", ww.Status()).
				Dur("duration", time.Since(start)).
				Msg("Request handled")
		}()

		next.ServeHTTP(ww, r)
	})
}

// handleMailbox handles POST /mailbox from peer sidecars.
func (s *Server) handleMailbox(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "failed to read body", err)
		return
	}

	var msg proto.MailboxMessage
	if err := goproto.Unmarshal(body, &msg); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid protobuf message", err)
		return
	}

	if err := s.coordinator.HandleMailboxMessage(r.Context(), &msg); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to handle mailbox message", err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// XTSubmitRequest represents a cross-chain transaction submission request.
type XTSubmitRequest struct {
	Transactions map[string]string `json:"transactions"` // chainID (string) -> hex-encoded RLP tx
}

// XTSubmitResponse represents the response to an XT submission.
type XTSubmitResponse struct {
	InstanceID string `json:"instance_id"`
	Status     string `json:"status"`
}

// handleXTSubmit handles POST /xt for cross-chain transaction submission.
func (s *Server) handleXTSubmit(w http.ResponseWriter, r *http.Request) {
	var req XTSubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	if len(req.Transactions) == 0 {
		s.writeError(w, http.StatusBadRequest, "no transactions provided", nil)
		return
	}

	// Convert string chainIDs to uint64 and decode hex transactions
	txs := make(map[uint64][]byte)
	for chainIDStr, hexTx := range req.Transactions {
		var chainID uint64
		if _, err := fmt.Sscanf(chainIDStr, "%d", &chainID); err != nil {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid chain_id: %s", chainIDStr), err)
			return
		}

		txBytes, err := hex.DecodeString(strings.TrimPrefix(hexTx, "0x"))
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid hex transaction", err)
			return
		}
		txs[chainID] = txBytes
	}

	instanceID, err := s.coordinator.SubmitXT(r.Context(), "", txs)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to submit XT", err)
		return
	}

	s.log.Info().
		Str("instance_id", instanceID).
		Int("chains", len(txs)).
		Msg("XT submitted via HTTP")

	s.writeJSON(w, http.StatusAccepted, XTSubmitResponse{
		InstanceID: instanceID,
		Status:     "pending",
	})
}

// handleXTStatus handles GET /xt/:instanceID for retrieving XT status.
func (s *Server) handleXTStatus(w http.ResponseWriter, r *http.Request) {
	instanceID := chi.URLParam(r, "instanceID")
	if instanceID == "" {
		s.writeError(w, http.StatusBadRequest, "instance_id is required", nil)
		return
	}

	status, err := s.coordinator.GetXTStatus(r.Context(), instanceID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "XT not found", err)
		return
	}

	s.writeJSON(w, http.StatusOK, status)
}

// XTForwardRequest represents a forwarded XT from a peer sidecar.
type XTForwardRequest = peer.XTForwardRequest

// handleXTForward handles POST /xt/forward for receiving forwarded XTs from peers.
func (s *Server) handleXTForward(w http.ResponseWriter, r *http.Request) {
	var req XTForwardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	if req.InstanceID == "" {
		s.writeError(w, http.StatusBadRequest, "instance_id is required", nil)
		return
	}

	// Convert string chainIDs to uint64 and decode hex transactions
	txs := make(map[uint64][]byte)
	for chainIDStr, hexTx := range req.Transactions {
		var chainID uint64
		if _, err := fmt.Sscanf(chainIDStr, "%d", &chainID); err != nil {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid chain_id: %s", chainIDStr), err)
			return
		}

		txBytes, err := hex.DecodeString(strings.TrimPrefix(hexTx, "0x"))
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid hex transaction", err)
			return
		}
		txs[chainID] = txBytes
	}

	// Forward to coordinator
	if err := s.coordinator.HandleForwardedXT(r.Context(), req.InstanceID, txs, req.OriginChain, req.OriginSeq); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to handle forwarded XT", err)
		return
	}

	s.log.Info().
		Str("instance_id", req.InstanceID).
		Int("chains", len(txs)).
		Uint64("origin_chain", req.OriginChain).
		Msg("Received forwarded XT from peer")

	w.WriteHeader(http.StatusOK)
}

// XTVoteRequest represents a vote from a peer sidecar.
type XTVoteRequest = peer.VoteRequest

// handleXTVote handles POST /xt/vote for receiving votes from peers.
func (s *Server) handleXTVote(w http.ResponseWriter, r *http.Request) {
	var req XTVoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	if req.InstanceID == "" {
		s.writeError(w, http.StatusBadRequest, "instance_id is required", nil)
		return
	}

	// Forward to coordinator
	if err := s.coordinator.HandlePeerVote(r.Context(), req.InstanceID, req.ChainID, req.Vote); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to handle vote", err)
		return
	}

	s.log.Debug().
		Str("instance_id", req.InstanceID).
		Uint64("chain_id", req.ChainID).
		Bool("vote", req.Vote).
		Msg("Received peer vote")

	w.WriteHeader(http.StatusOK)
}
