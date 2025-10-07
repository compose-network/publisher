package http

import (
	"net/http"

	"github.com/rs/zerolog"
	apicommon "github.com/ssvlabs/rollup-shared-publisher/server/api"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/batch"
)

type Handler struct {
	epochTracker  *batch.EpochTracker
	batchManager  *batch.Manager
	batchPipeline *batch.Pipeline
	log           zerolog.Logger
}

func NewHandler(
	epochTracker *batch.EpochTracker,
	batchManager *batch.Manager,
	batchPipeline *batch.Pipeline,
	log zerolog.Logger,
) *Handler {
	return &Handler{
		epochTracker:  epochTracker,
		batchManager:  batchManager,
		batchPipeline: batchPipeline,
		log:           log.With().Str("component", "batch-http").Logger(),
	}
}

// handleCurrentBatch returns the current batch information
func (h *Handler) handleCurrentBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if h.batchManager == nil {
		apicommon.WriteError(
			w, r,
			http.StatusServiceUnavailable,
			"batch_disabled",
			"Batch system is not enabled",
			nil,
		)
		return
	}

	currentBatch := h.batchManager.GetCurrentBatch()
	apicommon.WriteJSON(w, http.StatusOK, currentBatch)
}

// handleBatchStats returns batch manager statistics
func (h *Handler) handleBatchStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if h.batchManager == nil {
		apicommon.WriteError(
			w, r,
			http.StatusServiceUnavailable,
			"batch_disabled",
			"Batch system is not enabled",
			nil,
		)
		return
	}

	stats := h.batchManager.GetStats()
	apicommon.WriteJSON(w, http.StatusOK, stats)
}

// handleEpochStatus returns epoch tracker status
func (h *Handler) handleEpochStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if h.epochTracker == nil {
		apicommon.WriteError(
			w, r,
			http.StatusServiceUnavailable,
			"batch_disabled",
			"Batch system is not enabled",
			nil,
		)
		return
	}

	status := map[string]interface{}{
		"current_epoch": h.epochTracker.GetCurrentEpoch(),
		"current_slot":  h.epochTracker.GetCurrentSlot(),
		"batch_number":  h.epochTracker.GetCurrentBatchNumber(),
		"tracker_stats": h.epochTracker.GetStats(),
	}

	apicommon.WriteJSON(w, http.StatusOK, status)
}

// handlePipelineJobs returns active pipeline jobs
func (h *Handler) handlePipelineJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if h.batchPipeline == nil {
		apicommon.WriteError(
			w, r,
			http.StatusServiceUnavailable,
			"batch_disabled",
			"Batch system is not enabled",
			nil,
		)
		return
	}

	jobs := h.batchPipeline.GetActiveJobs()
	apicommon.WriteJSON(w, http.StatusOK, jobs)
}
