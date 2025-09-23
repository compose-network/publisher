package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

// RequestIDKey is the context key for request IDs.
const RequestIDKey contextKey = "request-id"

// RequestID middleware adds a unique request ID to each request.
func RequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get("X-Request-ID")

			if requestID == "" {
				b := make([]byte, 8)
				if _, err := rand.Read(b); err != nil {
					requestID = "req-error"
				} else {
					requestID = hex.EncodeToString(b)
				}
			}

			ctx := context.WithValue(r.Context(), RequestIDKey, requestID)

			w.Header().Set("X-Request-ID", requestID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
