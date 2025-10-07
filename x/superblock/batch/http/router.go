package http

import (
	"net/http"

	"github.com/gorilla/mux"
)

// Register binds stdlib mux routes.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc(routeCurrentBatch, h.handleCurrentBatch)
	mux.HandleFunc(routeBatchStats, h.handleBatchStats)
	mux.HandleFunc(routeEpochStatus, h.handleEpochStatus)
	mux.HandleFunc(routePipelineJobs, h.handlePipelineJobs)
}

// RegisterMux binds gorilla/mux routes.
func (h *Handler) RegisterMux(r *mux.Router) {
	r.HandleFunc(routeCurrentBatch, h.handleCurrentBatch).
		Methods(http.MethodGet).
		Name(routeNameCurrentBatch)

	r.HandleFunc(routeBatchStats, h.handleBatchStats).
		Methods(http.MethodGet).
		Name(routeNameBatchStats)

	r.HandleFunc(routeEpochStatus, h.handleEpochStatus).
		Methods(http.MethodGet).
		Name(routeNameEpochStatus)

	r.HandleFunc(routePipelineJobs, h.handlePipelineJobs).
		Methods(http.MethodGet).
		Name(routeNamePipelineJobs)
}
