package middleware

import (
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

// Logger provides structured access logging for HTTP requests.
func Logger(log zerolog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)
			log.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", rw.status).
				Int64("bytes", rw.bytes).
				Dur("latency", time.Since(start)).
				Msg("http_request")
		})
	}
}

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
