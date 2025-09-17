package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	apicommon "github.com/ssvlabs/rollup-shared-publisher/server/api"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs/collector"
)

type Handler struct {
	collector collector.Service
	log       zerolog.Logger
}

func NewHandler(collector collector.Service, log zerolog.Logger) *Handler {
	return &Handler{
		collector: collector,
		log:       log.With().Str("component", "proofs-http").Logger(),
	}
}

// handleSubmitAggregation handles the submission of aggregation proofs via a POST request
//
//nolint:gocyclo // ok, we can refactor this later
func (h *Handler) handleSubmitAggregation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	defer r.Body.Close()

	var req submitReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apicommon.WriteError(w, r, http.StatusBadRequest, "invalid_json", "failed to decode request", nil)
		return
	}

	// TODO: For testing, can skip hash validation by using a computed hash
	// sbHash := crypto.Keccak256Hash([]byte(fmt.Sprintf("superblock-%d", req.SuperblockNumber)))
	sbHashBytes, err := hexutil.Decode(req.SuperblockHash)
	if err != nil || len(sbHashBytes) != common.HashLength {
		apicommon.WriteError(
			w, r,
			http.StatusBadRequest,
			"invalid_superblock_hash",
			fmt.Sprintf("expect %d-byte hash", common.HashLength),
			nil,
		)
		return
	}
	sbHash := common.BytesToHash(sbHashBytes)

	l1HeadBytes, err := hexutil.Decode(req.L1Head)
	if err != nil || len(l1HeadBytes) != common.HashLength {
		apicommon.WriteError(
			w, r,
			http.StatusBadRequest,
			"invalid_l1_head",
			fmt.Sprintf("expect %d-byte hash", common.HashLength),
			nil,
		)
		return
	}
	l1Head := common.BytesToHash(l1HeadBytes)

	if !common.IsHexAddress(req.ProverAddress) {
		apicommon.WriteError(w, r, http.StatusBadRequest, "invalid_prover_address", "bad address", nil)
		return
	}
	prover := common.HexToAddress(req.ProverAddress)

	if req.Aggregation.L1Head == (common.Hash{}) {
		apicommon.WriteError(
			w, r,
			http.StatusBadRequest,
			"invalid_aggregation_outputs",
			"aggregation_outputs.l1Head is required",
			nil,
		)
		return
	}

	if req.Aggregation.L1Head != l1Head {
		apicommon.WriteError(
			w, r,
			http.StatusBadRequest,
			"invalid_aggregation_outputs",
			"aggregation_outputs.l1Head mismatch",
			nil,
		)
		return
	}

	if req.Aggregation.ProverAddress == (common.Address{}) {
		apicommon.WriteError(
			w, r,
			http.StatusBadRequest,
			"invalid_aggregation_outputs",
			"aggregation_outputs.proverAddress is required",
			nil,
		)
		return
	}

	if req.Aggregation.ProverAddress != prover {
		apicommon.WriteError(
			w, r,
			http.StatusBadRequest,
			"invalid_aggregation_outputs",
			"aggregation_outputs.proverAddress mismatch",
			nil,
		)
		return
	}

	if req.L2StartBlock > req.Aggregation.L2BlockNumber {
		apicommon.WriteError(
			w, r,
			http.StatusBadRequest,
			"invalid_l2_start_block",
			"l2_start_block must be <= aggregation_outputs.l2BlockNumber",
			nil,
		)
		return
	}

	aggVKTrim := bytes.TrimSpace(req.AggVK)
	if len(aggVKTrim) == 0 || bytes.Equal(aggVKTrim, []byte("null")) {
		apicommon.WriteError(w, r, http.StatusBadRequest, "missing_agg_vk", "agg_vk is required", nil)
		return
	}

	proofBytes := req.Proof.Clone()
	aggVK := append(json.RawMessage(nil), req.AggVK...)

	sub := proofs.Submission{
		SuperblockNumber: req.SuperblockNumber,
		SuperblockHash:   sbHash,
		ChainID:          req.ChainID,
		ProverAddress:    prover,
		L1Head:           l1Head,
		Aggregation:      req.Aggregation,
		L2StartBlock:     req.L2StartBlock,
		AggVerifyingKey:  aggVK,
		Proof:            proofBytes,
		ReceivedAt:       time.Now(),
	}

	if err := h.collector.SubmitOpSuccinct(r.Context(), sub); err != nil {
		apicommon.WriteError(w, r, http.StatusBadRequest, "submit_failed", err.Error(), nil)
		return
	}

	apicommon.WriteJSON(w, http.StatusAccepted, map[string]any{"status": "accepted"})
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	vars := mux.Vars(r)
	hashStr := strings.TrimSpace(vars["sbHash"])
	if hashStr == "" {
		apicommon.WriteError(
			w,
			r,
			http.StatusBadRequest,
			"missing_path_param",
			"provide /v1/proofs/status/{sbHash}",
			nil,
		)
		return
	}

	b, err := hexutil.Decode(hashStr)
	if err != nil || len(b) != common.HashLength {
		apicommon.WriteError(
			w, r,
			http.StatusBadRequest,
			"invalid_sbHash",
			fmt.Sprintf("expect %d-byte hash", common.HashLength),
			nil,
		)
		return
	}

	sbHash := common.BytesToHash(b)
	st, err := h.collector.GetStatus(r.Context(), sbHash)
	if err != nil {
		apicommon.WriteError(w, r, http.StatusNotFound, "not_found", err.Error(), nil)
		return
	}

	apicommon.WriteJSON(w, http.StatusOK, st)
}
