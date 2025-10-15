package prover

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/compose-network/publisher/x/superblock/proofs"
)

func TestHTTPClient_RequestProof(t *testing.T) {
	var captured []byte
	mock := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "/proof", req.URL.Path)
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		captured = body
		_ = req.Body.Close()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(
				bytes.NewReader([]byte(`{"success":true,"message":"queued","request_id":"job-123"}`)),
			),
			Header: make(http.Header),
		}, nil
	})

	client, err := NewHTTPClient("http://example.com", &http.Client{Transport: mock}, zerolog.Nop())
	require.NoError(t, err)

	job := proofs.ProofJobInput{ProofType: "groth16", Input: proofs.SuperblockProverInput{}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	jobID, err := client.RequestProof(ctx, job)
	require.NoError(t, err)
	require.Equal(t, "job-123", jobID)

	var sent proofs.ProofJobInput
	require.NoError(t, json.Unmarshal(captured, &sent))
	require.Equal(t, job.ProofType, sent.ProofType)
}

func TestHTTPClient_GetStatus(t *testing.T) {
	sampleProof := proofs.ProofBytes{1, 2, 3}
	reply := statusResponse{
		Success: true,
		Status:  "completed",
		Result: &statusResult{
			Proof:         sampleProof,
			ProvingTimeMs: ptrUint64(1234),
			Cycles:        ptrUint64(5678),
		},
	}
	encoded, err := json.Marshal(reply)
	require.NoError(t, err)

	mock := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "/proof/job-xyz", req.URL.Path)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(encoded)),
			Header:     make(http.Header),
		}, nil
	})

	client, err := NewHTTPClient("http://example.com", &http.Client{Transport: mock}, zerolog.Nop())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, err := client.GetStatus(ctx, "job-xyz")
	require.NoError(t, err)
	require.Equal(t, "completed", status.Status)
	require.Equal(t, []byte(sampleProof), status.Proof)
	require.NotNil(t, status.ProvingTimeMS)
	require.Equal(t, uint64(1234), *status.ProvingTimeMS)
	require.NotNil(t, status.Cycles)
	require.Equal(t, uint64(5678), *status.Cycles)
}

func TestHTTPClient_RequestProofError(t *testing.T) {
	mock := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(bytes.NewReader([]byte("bad request"))),
		}, nil
	})

	client, err := NewHTTPClient("http://example.com", &http.Client{Transport: mock}, zerolog.Nop())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = client.RequestProof(ctx, proofs.ProofJobInput{})
	require.Error(t, err)
}

func TestHTTPClient_GetStatusError(t *testing.T) {
	mock := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(bytes.NewReader([]byte("boom"))),
		}, nil
	})

	client, err := NewHTTPClient("http://example.com", &http.Client{Transport: mock}, zerolog.Nop())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = client.GetStatus(ctx, "job")
	require.Error(t, err)
}

func ptrUint64(v uint64) *uint64 { return &v }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
