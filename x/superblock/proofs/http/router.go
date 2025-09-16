package http

import (
	"net/http"

	"github.com/gorilla/mux"
)

// Register binds stdlib mux routes.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc(routeSubmitOpSuccinct, h.handleSubmitAggregation)
}

// RegisterMux binds gorilla/mux routes.
func (h *Handler) RegisterMux(r *mux.Router) {
	r.HandleFunc(routeSubmitOpSuccinct, h.handleSubmitAggregation).
		Methods(http.MethodPost).
		Name(routeNameSubmitOpSuccinct)
	r.HandleFunc(routeStatusByHash, h.handleStatus).Methods(http.MethodGet).Name(routeNameStatusByHash)
}
