package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs/collector"
	"github.com/stretchr/testify/require"
)

func TestHandler_SubmitAndStatus_OK(t *testing.T) {
	h := NewHandler(collector.New(t.Context(), zerolog.New(io.Discard)), zerolog.New(io.Discard))
	r := mux.NewRouter()
	h.RegisterMux(r)

	sbHash := "0x" + strings.Repeat("aa", 32)
	proverAddr := "0x0123456789abcdef0123456789abcdef01234567"
	l1Head := "0x" + strings.Repeat("bb", 32)
	aggOutputs := map[string]any{
		"l1Head":           l1Head,
		"l2PreRoot":        "0x" + strings.Repeat("cc", 32),
		"l2PostRoot":       "0x" + strings.Repeat("dd", 32),
		"l2BlockNumber":    1005,
		"rollupConfigHash": "0x" + strings.Repeat("ee", 32),
		"multiBlockVKey":   "0x" + strings.Repeat("ff", 32),
		"proverAddress":    proverAddr,
	}
	body := map[string]any{
		"superblock_number":   1,
		"superblock_hash":     sbHash,
		"chain_id":            11155111,
		"prover_address":      proverAddr,
		"l1_head":             l1Head,
		"aggregation_outputs": aggOutputs,
		"l2_start_block":      900,
		"agg_vk": map[string]any{
			"vk": map[string]any{"commit": []int{1, 2, 3}},
		},
		"proof": []int{1, 2, 3},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, routeSubmitOpSuccinct, bytes.NewReader(b))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)

	u, err := r.Get(routeNameStatusByHash).URL("sbHash", sbHash)
	require.NoError(t, err)
	req2 := httptest.NewRequest(http.MethodGet, u.String(), nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
}
