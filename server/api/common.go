package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ssvlabs/rollup-shared-publisher/server/api/middleware"
)

// WriteError writes a standardized error response with request tracking.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string, details any) {
	requestID, _ := r.Context().Value(middleware.RequestIDKey).(string)

	response := map[string]any{
		"error": map[string]any{
			"code":       code,
			"message":    message,
			"request_id": requestID,
			"timestamp":  time.Now().UTC().Format(time.RFC3339),
		},
	}

	if details != nil {
		response["error"].(map[string]any)["details"] = details
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

// WriteJSON writes a JSON response.
func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
