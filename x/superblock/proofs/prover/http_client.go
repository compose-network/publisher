package prover

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/rs/zerolog"

	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs"
)

// HTTPClient implements proofs.ProverClient over the superblock-prover REST API.
type HTTPClient struct {
	baseURL    *url.URL
	httpClient *http.Client
	log        zerolog.Logger
}

// NewHTTPClient constructs a prover client for the given base URL.
func NewHTTPClient(rawURL string, httpClient *http.Client, log zerolog.Logger) (*HTTPClient, error) {
	if rawURL == "" {
		return nil, errors.New("base URL is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid prover base URL: %w", err)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	logger := log.With().Str("component", "prover-client").Logger()

	client := &HTTPClient{
		baseURL:    parsed,
		httpClient: httpClient,
		log:        logger,
	}

	logger.Info().
		Str("base_url", rawURL).
		Dur("timeout", httpClient.Timeout).
		Msg("HTTP prover client initialized")

	return client, nil
}

// RequestProof submits a proof generation job to the prover service.
func (c *HTTPClient) RequestProof(ctx context.Context, job proofs.ProofJobInput) (string, error) {
	endpoint := c.buildURL("proof")

	c.log.Info().
		Str("endpoint", endpoint).
		Interface("job", job).
		Msg("requesting proof generation")

	body, err := json.Marshal(job)
	if err != nil {
		c.log.Error().Err(err).Msg("failed to marshal proof job")
		return "", fmt.Errorf("marshal proof job: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		c.log.Error().Err(err).Msg("failed to prepare proof request")
		return "", fmt.Errorf("prepare request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		c.log.Error().Err(err).Str("endpoint", endpoint).Msg("proof request failed")
		return "", fmt.Errorf("post proof request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		c.log.Error().
			Int("status_code", res.StatusCode).
			Str("status", res.Status).
			Str("response", string(msg)).
			Msg("prover returned error response")
		return "", fmt.Errorf("prover returned %s: %s", res.Status, string(msg))
	}

	var submission submissionResponse
	if err := json.NewDecoder(res.Body).Decode(&submission); err != nil {
		c.log.Error().Err(err).Msg("failed to decode prover response")
		return "", fmt.Errorf("decode prover response: %w", err)
	}

	if !submission.Success {
		errMsg := submission.errorMessage()
		c.log.Error().Str("error", errMsg).Msg("prover rejected proof job")
		return "", fmt.Errorf("prover rejected job: %s", errMsg)
	}

	if submission.RequestID == "" {
		c.log.Error().Msg("prover response missing request_id")
		return "", errors.New("prover response missing request_id")
	}

	c.log.Info().
		Str("job_id", submission.RequestID).
		Str("message", submission.Message).
		Msg("proof job submitted successfully")

	return submission.RequestID, nil
}

// GetStatus fetches the status of a previously submitted job.
func (c *HTTPClient) GetStatus(ctx context.Context, jobID string) (proofs.ProofJobStatus, error) {
	if jobID == "" {
		c.log.Error().Msg("job ID is required for status check")
		return proofs.ProofJobStatus{}, errors.New("jobID is required")
	}

	endpoint := c.buildURL(path.Join("proof", jobID))

	c.log.Info().
		Str("job_id", jobID).
		Str("endpoint", endpoint).
		Msg("checking proof job status")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		c.log.Error().Err(err).Str("job_id", jobID).Msg("failed to prepare status request")
		return proofs.ProofJobStatus{}, fmt.Errorf("prepare status request: %w", err)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		c.log.Error().Err(err).Str("job_id", jobID).Str("endpoint", endpoint).Msg("status request failed")
		return proofs.ProofJobStatus{}, fmt.Errorf("get proof status: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		c.log.Error().
			Str("job_id", jobID).
			Int("status_code", res.StatusCode).
			Str("response", string(msg)).
			Msg("prover returned error for status check")
		return proofs.ProofJobStatus{}, fmt.Errorf("prover returned %s: %s", res.Status, string(msg))
	}

	var status statusResponse
	if err := json.NewDecoder(res.Body).Decode(&status); err != nil {
		c.log.Error().Err(err).Str("job_id", jobID).Msg("failed to decode status response")
		return proofs.ProofJobStatus{}, fmt.Errorf("decode status response: %w", err)
	}

	if !status.Success {
		errMsg := status.errorMessage()
		c.log.Error().Str("job_id", jobID).Str("error", errMsg).Msg("prover reported job failure")
		if errMsg == "" {
			return proofs.ProofJobStatus{}, errors.New("prover returned unsuccessful status")
		}
		return proofs.ProofJobStatus{}, fmt.Errorf("prover reported failure: %s", errMsg)
	}

	result := proofs.ProofJobStatus{Status: status.Status}
	if status.Result != nil {
		if len(status.Result.Proof) > 0 {
			result.Proof = status.Result.Proof.Clone()
		}
		result.ProvingTimeMS = status.Result.ProvingTimeMs
		result.Cycles = status.Result.Cycles
	}

	c.log.Info().
		Str("job_id", jobID).
		Str("status", result.Status).
		Interface("proving_time_ms", result.ProvingTimeMS).
		Interface("cycles", result.Cycles).
		Bool("has_proof", len(result.Proof) > 0).
		Msg("retrieved proof job status")

	return result, nil
}

func (c *HTTPClient) buildURL(elem ...string) string {
	clone := *c.baseURL
	clone.Path = path.Join(append([]string{c.baseURL.Path}, elem...)...)
	return clone.String()
}

type submissionResponse struct {
	Success   bool    `json:"success"`
	Message   string  `json:"message"`
	RequestID string  `json:"request_id"`
	Error     *string `json:"error"`
}

func (r submissionResponse) errorMessage() string {
	if r.Error != nil {
		return *r.Error
	}
	return r.Message
}

type statusResponse struct {
	Success bool          `json:"success"`
	Status  string        `json:"status"`
	Result  *statusResult `json:"result"`
	Error   *string       `json:"error"`
}

func (r statusResponse) errorMessage() string {
	if r.Error != nil {
		return *r.Error
	}
	return ""
}

type statusResult struct {
	Proof         proofs.ProofBytes `json:"proof"`
	ProvingTimeMs *uint64           `json:"proving_time_ms"`
	Cycles        *uint64           `json:"cycles"`
}

// Ensure HTTPClient satisfies proofs.ProverClient at compile time.
var _ proofs.ProverClient = (*HTTPClient)(nil)
