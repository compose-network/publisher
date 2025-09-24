package batch

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		config    Config
		wantValid bool
		wantError string
	}{
		{
			name:      "disabled config is always valid",
			config:    Config{Enabled: false},
			wantValid: true,
		},
		{
			name: "valid config",
			config: Config{
				Enabled: true,
				L1Listener: ListenerConfig{
					L1RPC:        "http://localhost:8545",
					BatchFactor:  10,
					PollInterval: 12 * time.Second,
				},
				BatchManager: ManagerConfig{
					ChainID:      1001,
					MaxBatchSize: 100,
					BatchTimeout: 60 * time.Minute,
				},
				Pipeline: PipelineConfig{
					MaxConcurrentJobs: 5,
					JobTimeout:        30 * time.Minute,
					MaxRetries:        3,
					RetryDelay:        5 * time.Minute,
				},
				Integration: IntegrationConfig{
					ChainID: 1001,
				},
			},
			wantValid: true,
		},
		{
			name: "missing L1 RPC",
			config: Config{
				Enabled: true,
				L1Listener: ListenerConfig{
					BatchFactor:  10,
					PollInterval: 12 * time.Second,
				},
			},
			wantValid: false,
			wantError: "l1_listener.l1_rpc is required",
		},
		{
			name: "mismatched chain IDs",
			config: Config{
				Enabled: true,
				L1Listener: ListenerConfig{
					L1RPC:        "http://localhost:8545",
					BatchFactor:  10,
					PollInterval: 12 * time.Second,
				},
				BatchManager: ManagerConfig{
					ChainID:      1001,
					MaxBatchSize: 100,
					BatchTimeout: 60 * time.Minute,
				},
				Pipeline: PipelineConfig{
					MaxConcurrentJobs: 5,
					JobTimeout:        30 * time.Minute,
					MaxRetries:        3,
					RetryDelay:        5 * time.Minute,
				},
				Integration: IntegrationConfig{
					ChainID: 1002, // Different chain ID
				},
			},
			wantValid: false,
			wantError: "chain_id and integration.chain_id must match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.config.Validate()

			if tt.wantValid {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
			}
		})
	}
}

func TestConfig_ApplyDefaults(t *testing.T) {
	t.Parallel()

	config := Config{
		Enabled: true,
	}

	config.ApplyDefaults()

	assert.Equal(t, DefaultListenerConfig().BatchFactor, config.L1Listener.BatchFactor)
	assert.Equal(t, DefaultManagerConfig().MaxBatchSize, config.BatchManager.MaxBatchSize)
	assert.Equal(t, DefaultPipelineConfig().MaxConcurrentJobs, config.Pipeline.MaxConcurrentJobs)
}

func TestConfig_IsProductionReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		config     Config
		wantReady  bool
		wantIssues int
	}{
		{
			name:       "disabled config not ready",
			config:     Config{Enabled: false},
			wantReady:  false,
			wantIssues: 1,
		},
		{
			name:       "production config is ready",
			config:     GetRecommendedProductionConfig(1001, "https://mainnet.infura.io/v3/test"),
			wantReady:  true,
			wantIssues: 0,
		},
		{
			name:       "test config has issues",
			config:     GetTestConfig(1001),
			wantReady:  false,
			wantIssues: 3, // Short timeouts and local RPC
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ready, issues := tt.config.IsProductionReady()

			assert.Equal(t, tt.wantReady, ready)
			assert.Len(t, issues, tt.wantIssues)
		})
	}
}

func TestBatchInfo_Lifecycle(t *testing.T) {
	t.Parallel()

	// For this test, we'll test the batch info structure itself
	batch := &BatchInfo{
		ID:         1,
		State:      StateCollecting,
		StartEpoch: 100,
		StartTime:  time.Now(),
		StartSlot:  1000,
		ChainID:    1001,
		Blocks:     make([]BatchBlockInfo, 0),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	// Test initial state
	assert.Equal(t, uint64(1), batch.ID)
	assert.Equal(t, StateCollecting, batch.State)
	assert.Equal(t, uint32(1001), batch.ChainID)
	assert.Empty(t, batch.Blocks)

	// Test adding blocks
	block1 := BatchBlockInfo{
		SlotNumber:  1001,
		BlockNumber: 2001,
		BlockHash:   common.HexToHash("0x1234"),
		Timestamp:   time.Now(),
		TxCount:     5,
	}

	batch.Blocks = append(batch.Blocks, block1)
	batch.SlotCount = 1
	batch.UpdatedAt = time.Now()

	assert.Len(t, batch.Blocks, 1)
	assert.Equal(t, uint64(1001), batch.Blocks[0].SlotNumber)
	assert.Equal(t, 5, batch.Blocks[0].TxCount)
}

func TestPipelineJob_Lifecycle(t *testing.T) {
	t.Parallel()

	job := &PipelineJob{
		ID:        "test-job-1",
		BatchID:   1,
		ChainID:   1001,
		Stage:     StageRangeProof,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Test initial state
	assert.Equal(t, "test-job-1", job.ID)
	assert.Equal(t, StageRangeProof, job.Stage)
	assert.Nil(t, job.RangeProof)
	assert.Equal(t, 0, job.RetryCount)

	// Test stage progression
	job.Stage = StageAggregation
	assert.Equal(t, StageAggregation, job.Stage)

	// Test error handling
	errMsg := "test error"
	job.ErrorMessage = &errMsg
	job.RetryCount = 1

	assert.Equal(t, "test error", *job.ErrorMessage)
	assert.Equal(t, 1, job.RetryCount)

	// Test completion
	job.Stage = StageCompleted
	job.ErrorMessage = nil

	assert.Equal(t, StageCompleted, job.Stage)
	assert.Nil(t, job.ErrorMessage)
}

func TestBatchEvent_Creation(t *testing.T) {
	t.Parallel()

	event := BatchEvent{
		Type:      "batch_started",
		BatchID:   1,
		Data:      map[string]interface{}{"test": "value"},
		Timestamp: time.Now(),
	}

	assert.Equal(t, "batch_started", event.Type)
	assert.Equal(t, uint64(1), event.BatchID)
	assert.NotNil(t, event.Data)
	assert.False(t, event.Timestamp.IsZero())
}

func TestPipelineJobEvent_Creation(t *testing.T) {
	t.Parallel()

	event := PipelineJobEvent{
		Type:      "job_created",
		JobID:     "test-job-1",
		BatchID:   1,
		Stage:     StageRangeProof,
		Data:      map[string]string{"status": "started"},
		Timestamp: time.Now(),
	}

	assert.Equal(t, "job_created", event.Type)
	assert.Equal(t, "test-job-1", event.JobID)
	assert.Equal(t, uint64(1), event.BatchID)
	assert.Equal(t, StageRangeProof, event.Stage)
	assert.NotNil(t, event.Data)
}

func TestDefaultConfigs(t *testing.T) {
	t.Parallel()

	// Test that all default configs are valid when properly set up
	cfg := DefaultConfig()
	cfg.SetChainID(1001)
	cfg.SetL1RPC("http://localhost:8545")

	err := cfg.Validate()
	assert.NoError(t, err)

	// Test individual default configs
	listenerCfg := DefaultListenerConfig()
	assert.Equal(t, uint64(10), listenerCfg.BatchFactor)
	assert.Equal(t, 12*time.Second, listenerCfg.PollInterval)

	managerCfg := DefaultManagerConfig()
	assert.Equal(t, uint64(320), managerCfg.MaxBatchSize)
	assert.Equal(t, 90*time.Minute, managerCfg.BatchTimeout)

	pipelineCfg := DefaultPipelineConfig()
	assert.Equal(t, 5, pipelineCfg.MaxConcurrentJobs)
	assert.Equal(t, 30*time.Minute, pipelineCfg.JobTimeout)
	assert.Equal(t, 3, pipelineCfg.MaxRetries)

	integrationCfg := DefaultIntegrationConfig()
	assert.True(t, integrationCfg.EnableBatchSync)
	assert.True(t, integrationCfg.BlockReporting)
}

func TestConfigHelpers(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

	// Test SetChainID
	cfg.SetChainID(1001)
	assert.Equal(t, uint32(1001), cfg.BatchManager.ChainID)
	assert.Equal(t, uint32(1001), cfg.Integration.ChainID)

	// Test SetL1RPC
	cfg.SetL1RPC("https://test.rpc")
	assert.Equal(t, "https://test.rpc", cfg.L1Listener.L1RPC)

	// Test SetBatchFactor
	cfg.SetBatchFactor(20)
	assert.Equal(t, uint64(20), cfg.L1Listener.BatchFactor)

	// Test DisableBatchSync
	cfg.DisableBatchSync()
	assert.False(t, cfg.Enabled)
	assert.False(t, cfg.Integration.EnableBatchSync)

	// Test EnableBatchSync
	cfg.SetL1RPC("http://localhost:8545") // Set valid RPC first
	err := cfg.EnableBatchSync()
	assert.NoError(t, err)
	assert.True(t, cfg.Enabled)
	assert.True(t, cfg.Integration.EnableBatchSync)
}

func TestGetSummary(t *testing.T) {
	t.Parallel()

	// Test disabled config
	cfg := Config{Enabled: false}
	summary := cfg.GetSummary()
	assert.Equal(t, false, summary["enabled"])

	// Test enabled config
	cfg = GetTestConfig(1001)
	summary = cfg.GetSummary()
	assert.Equal(t, true, summary["enabled"])
	assert.Contains(t, summary, "l1_listener")
	assert.Contains(t, summary, "batch_manager")
	assert.Contains(t, summary, "pipeline")
	assert.Contains(t, summary, "integration")
}
