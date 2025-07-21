package publisher

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

// HTTPHandler provides HTTP endpoints.
type HTTPHandler struct {
	publisher *Publisher
	log       zerolog.Logger
	startTime time.Time
}

// NewHTTPHandler creates a new HTTP handler.
func NewHTTPHandler(p *Publisher, log zerolog.Logger) *HTTPHandler {
	return &HTTPHandler{
		publisher: p,
		log:       log.With().Str("component", "http").Logger(),
		startTime: time.Now(),
	}
}

// RegisterRoutes registers HTTP routes.
func (h *HTTPHandler) RegisterRoutes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", h.handleHealth)
	mux.HandleFunc("/ready", h.handleReady)
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/stats", h.handleStats)
	mux.HandleFunc("/connections", h.handleConnections)
	mux.HandleFunc("/debug/vars", h.handleDebugVars)

	return h.loggingMiddleware(mux)
}

// loggingMiddleware logs HTTP requests.
func (h *HTTPHandler) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		h.log.Debug().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("remote", r.RemoteAddr).
			Int("status", wrapped.statusCode).
			Dur("duration", time.Since(start)).
			Msg("HTTP request")
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// handleHealth returns health status.
func (h *HTTPHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status": "healthy",
		"uptime": time.Since(h.startTime).String(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleReady returns readiness status.
func (h *HTTPHandler) handleReady(w http.ResponseWriter, r *http.Request) {
	stats := h.publisher.GetStats()
	connections := stats["active_connections"].(int)

	status := "ready"
	code := http.StatusOK

	if connections == 0 {
		status = "no_connections"
	}

	response := map[string]interface{}{
		"status":      status,
		"connections": connections,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(response)
}

// handleStats returns publisher statistics.
func (h *HTTPHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := h.publisher.GetStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleConnections returns connection details.
func (h *HTTPHandler) handleConnections(w http.ResponseWriter, r *http.Request) {
	connections := h.publisher.server.GetConnections()

	response := map[string]interface{}{
		"count":       len(connections),
		"connections": connections,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleDebugVars returns debug variables.
func (h *HTTPHandler) handleDebugVars(w http.ResponseWriter, r *http.Request) {
	stats := h.publisher.GetStats()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	debug := map[string]interface{}{
		"publisher": stats,
		"runtime": map[string]interface{}{
			"goroutines":   runtime.NumGoroutine(),
			"memory_alloc": m.Alloc,
			"memory_sys":   m.Sys,
			"gc_runs":      m.NumGC,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(debug)
}
