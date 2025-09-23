package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// responseWriter wraps http.ResponseWriter to capture status and bytes.
type responseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *responseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Logger provides structured access logging for HTTP requests.
func Logger(log zerolog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isNoiseEndpoint(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			requestID, _ := r.Context().Value(RequestIDKey).(string)
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rw, r)

			duration := time.Since(start)

			var evt *zerolog.Event
			switch {
			case rw.status >= 500:
				evt = log.Error()
			case rw.status >= 400:
			case duration > 5*time.Second:
				evt = log.Warn()
			default:
				evt = log.Info()
			}

			evt = evt.
				Str("request_id", requestID).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", rw.status).
				Int64("bytes", rw.bytes).
				Dur("latency", duration).
				Str("ip", getClientIP(r))

			if r.URL.RawQuery != "" {
				evt = evt.Str("query", r.URL.RawQuery)
			}

			if rw.status >= 400 {
				evt = evt.Str("user_agent", r.UserAgent())
			}

			evt.Msg("http_request")
		})
	}
}

// isNoiseEndpoint checks if a given path is a known non-essential or noise endpoint like health checks or metrics.
func isNoiseEndpoint(path string) bool {
	return path == "/health" ||
		path == "/healthz" ||
		path == "/ready" ||
		path == "/live" ||
		path == "/metrics" ||
		path == "/favicon.ico"
}

// getClientIP extracts the client's IP address from an HTTP request.
// It prioritizes headers (X-Forwarded-For, X-Real-IP) and falls back to RemoteAddr.
func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return xff
	}

	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	return r.RemoteAddr
}
