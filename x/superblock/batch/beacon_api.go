package batch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// TODO: use a more robust library
// BeaconResponse wraps beacon chain API responses
type BeaconResponse struct {
	Data json.RawMessage `json:"data"`
}

// BeaconEpochResponse represents epoch information from beacon API
type BeaconEpochResponse struct {
	Epoch          string `json:"epoch"`
	BlockNumber    string `json:"execution_block_number"`
	BlockHash      string `json:"execution_block_hash"`
	Timestamp      string `json:"timestamp"`
	FinalizedEpoch string `json:"finalized_epoch"`
}

// BeaconSlotResponse represents slot information from beacon API
type BeaconSlotResponse struct {
	Slot             string           `json:"slot"`
	Epoch            string           `json:"epoch"`
	ExecutionPayload ExecutionPayload `json:"execution_payload"`
}

// ExecutionPayload represents execution layer information
type ExecutionPayload struct {
	BlockNumber string `json:"block_number"`
	BlockHash   string `json:"block_hash"`
	Timestamp   string `json:"timestamp"`
}

// GetCurrentEpoch fetches current epoch from beacon chain API
func (b *BeaconChainAPI) GetCurrentEpoch(ctx context.Context) (*BeaconEpochData, error) {
	url := fmt.Sprintf("%s/eth/v1/beacon/headers/head", b.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("beacon API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("beacon API returned %d", resp.StatusCode)
	}

	var beaconResp BeaconResponse
	if err := json.NewDecoder(resp.Body).Decode(&beaconResp); err != nil {
		return nil, fmt.Errorf("decode beacon response: %w", err)
	}

	var slotResp BeaconSlotResponse
	if err := json.Unmarshal(beaconResp.Data, &slotResp); err != nil {
		return nil, fmt.Errorf("decode slot data: %w", err)
	}

	// Convert slot to epoch (slot / 32)
	slot := parseUint64(slotResp.Slot)
	epoch := slot / 32
	blockNumber := parseUint64(slotResp.ExecutionPayload.BlockNumber)
	timestamp := parseUint64(slotResp.ExecutionPayload.Timestamp)

	data := &BeaconEpochData{
		Epoch:       epoch,
		BlockNumber: blockNumber,
		BlockHash:   slotResp.ExecutionPayload.BlockHash,
		Timestamp:   timestamp,
	}

	b.log.Debug().
		Uint64("epoch", epoch).
		Uint64("slot", slot).
		Uint64("block_number", blockNumber).
		Msg("Retrieved current epoch from beacon API")

	return data, nil
}

// GetEpochData fetches specific epoch information
func (b *BeaconChainAPI) GetEpochData(ctx context.Context, epoch uint64) (*BeaconEpochData, error) {
	// Get the first slot of the epoch
	slot := epoch * 32
	url := fmt.Sprintf("%s/eth/v2/beacon/blocks/%d", b.baseURL, slot)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("beacon API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("beacon API returned %d for epoch %d", resp.StatusCode, epoch)
	}

	var beaconResp BeaconResponse
	if err := json.NewDecoder(resp.Body).Decode(&beaconResp); err != nil {
		return nil, fmt.Errorf("decode beacon response: %w", err)
	}

	var blockResp BeaconBlockResponse
	if err := json.Unmarshal(beaconResp.Data, &blockResp); err != nil {
		return nil, fmt.Errorf("decode block data: %w", err)
	}

	blockNumber := parseUint64(blockResp.Message.Body.ExecutionPayload.BlockNumber)
	timestamp := parseUint64(blockResp.Message.Body.ExecutionPayload.Timestamp)

	data := &BeaconEpochData{
		Epoch:       epoch,
		BlockNumber: blockNumber,
		BlockHash:   blockResp.Message.Body.ExecutionPayload.BlockHash,
		Timestamp:   timestamp,
	}

	b.log.Debug().
		Uint64("epoch", epoch).
		Uint64("slot", slot).
		Uint64("block_number", blockNumber).
		Msg("Retrieved epoch data from beacon API")

	return data, nil
}

// GetFinalizedEpoch fetches the latest finalized epoch
func (b *BeaconChainAPI) GetFinalizedEpoch(ctx context.Context) (uint64, error) {
	url := fmt.Sprintf("%s/eth/v1/beacon/states/finalized/finality_checkpoints", b.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("beacon API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("beacon API returned %d", resp.StatusCode)
	}

	var beaconResp BeaconResponse
	if err := json.NewDecoder(resp.Body).Decode(&beaconResp); err != nil {
		return 0, fmt.Errorf("decode beacon response: %w", err)
	}

	var checkpoints FinalityCheckpoints
	if err := json.Unmarshal(beaconResp.Data, &checkpoints); err != nil {
		return 0, fmt.Errorf("decode finality data: %w", err)
	}

	finalizedEpoch := parseUint64(checkpoints.Finalized.Epoch)

	b.log.Debug().
		Uint64("finalized_epoch", finalizedEpoch).
		Msg("Retrieved finalized epoch from beacon API")

	return finalizedEpoch, nil
}

// BeaconBlockResponse represents a beacon block response
type BeaconBlockResponse struct {
	Message BeaconBlockMessage `json:"message"`
}

// BeaconBlockMessage represents the beacon block message
type BeaconBlockMessage struct {
	Slot uint64          `json:"slot,string"`
	Body BeaconBlockBody `json:"body"`
}

// BeaconBlockBody represents the beacon block body
type BeaconBlockBody struct {
	ExecutionPayload ExecutionPayload `json:"execution_payload"`
}

// FinalityCheckpoints represents finality checkpoint data
type FinalityCheckpoints struct {
	Finalized Checkpoint `json:"finalized"`
	Justified Checkpoint `json:"justified"`
}

// Checkpoint represents a beacon chain checkpoint
type Checkpoint struct {
	Epoch string `json:"epoch"`
	Root  string `json:"root"`
}

// parseUint64 safely parses string to uint64
func parseUint64(s string) uint64 {
	if s == "" {
		return 0
	}

	var result uint64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			result = result*10 + uint64(c-'0')
		}
	}
	return result
}

// HealthCheck verifies beacon API connectivity
func (b *BeaconChainAPI) HealthCheck(ctx context.Context) error {
	url := fmt.Sprintf("%s/eth/v1/node/health", b.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("create health check request: %w", err)
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("beacon API health check failed with status %d", resp.StatusCode)
	}

	b.log.Info().Msg("Beacon API health check passed")
	return nil
}
