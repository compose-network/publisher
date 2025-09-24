package batch

import (
	"fmt"
	"time"
)

// Config holds all batch synchronization configuration
type Config struct {
	// L1 Listener configuration
	L1Listener ListenerConfig `mapstructure:"l1_listener" yaml:"l1_listener"`

	// Batch Manager configuration
	BatchManager ManagerConfig `mapstructure:"batch_manager" yaml:"batch_manager"`

	// Proof Pipeline configuration
	Pipeline PipelineConfig `mapstructure:"pipeline" yaml:"pipeline"`

	// Sequencer Integration configuration
	Integration IntegrationConfig `mapstructure:"integration" yaml:"integration"`

	// Global batch settings
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`
}

// DefaultConfig returns the default batch synchronization configuration
func DefaultConfig() Config {
	return Config{
		Enabled:      true,
		L1Listener:   DefaultListenerConfig(),
		BatchManager: DefaultManagerConfig(),
		Pipeline:     DefaultPipelineConfig(),
		Integration:  DefaultIntegrationConfig(),
	}
}

// Validate checks that the configuration is valid
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil // Skip validation if disabled
	}

	// Validate L1 Listener
	if c.L1Listener.L1RPC == "" {
		return fmt.Errorf("l1_listener.l1_rpc is required when batch sync is enabled")
	}
	if c.L1Listener.BatchFactor == 0 {
		return fmt.Errorf("l1_listener.batch_factor must be greater than 0")
	}
	if c.L1Listener.PollInterval <= 0 {
		return fmt.Errorf("l1_listener.poll_interval must be greater than 0")
	}

	// Validate Batch Manager
	if c.BatchManager.ChainID == 0 {
		return fmt.Errorf("batch_manager.chain_id must be specified")
	}
	if c.BatchManager.MaxBatchSize == 0 {
		return fmt.Errorf("batch_manager.max_batch_size must be greater than 0")
	}
	if c.BatchManager.BatchTimeout <= 0 {
		return fmt.Errorf("batch_manager.batch_timeout must be greater than 0")
	}

	// Validate Pipeline
	if c.Pipeline.MaxConcurrentJobs <= 0 {
		return fmt.Errorf("pipeline.max_concurrent_jobs must be greater than 0")
	}
	if c.Pipeline.JobTimeout <= 0 {
		return fmt.Errorf("pipeline.job_timeout must be greater than 0")
	}
	if c.Pipeline.MaxRetries < 0 {
		return fmt.Errorf("pipeline.max_retries cannot be negative")
	}
	if c.Pipeline.RetryDelay <= 0 {
		return fmt.Errorf("pipeline.retry_delay must be greater than 0")
	}

	// Validate Integration
	if c.Integration.ChainID == 0 {
		return fmt.Errorf("integration.chain_id must be specified")
	}

	// Cross-validation
	if c.BatchManager.ChainID != c.Integration.ChainID {
		return fmt.Errorf("batch_manager.chain_id and integration.chain_id must match")
	}

	return nil
}

// GetSummary returns a summary of the configuration for logging
func (c *Config) GetSummary() map[string]interface{} {
	if !c.Enabled {
		return map[string]interface{}{
			"enabled": false,
		}
	}

	return map[string]interface{}{
		"enabled": true,
		"l1_listener": map[string]interface{}{
			"batch_factor":  c.L1Listener.BatchFactor,
			"poll_interval": c.L1Listener.PollInterval.String(),
			"has_rpc_url":   c.L1Listener.L1RPC != "",
		},
		"batch_manager": map[string]interface{}{
			"chain_id":       c.BatchManager.ChainID,
			"max_batch_size": c.BatchManager.MaxBatchSize,
			"batch_timeout":  c.BatchManager.BatchTimeout.String(),
		},
		"pipeline": map[string]interface{}{
			"max_concurrent_jobs": c.Pipeline.MaxConcurrentJobs,
			"job_timeout":         c.Pipeline.JobTimeout.String(),
			"max_retries":         c.Pipeline.MaxRetries,
		},
		"integration": map[string]interface{}{
			"chain_id":          c.Integration.ChainID,
			"enable_batch_sync": c.Integration.EnableBatchSync,
			"block_reporting":   c.Integration.BlockReporting,
		},
	}
}

// ApplyDefaults fills in any missing configuration with defaults
func (c *Config) ApplyDefaults() {
	// L1 Listener defaults
	if c.L1Listener.BatchFactor == 0 {
		c.L1Listener.BatchFactor = DefaultListenerConfig().BatchFactor
	}
	if c.L1Listener.PollInterval == 0 {
		c.L1Listener.PollInterval = DefaultListenerConfig().PollInterval
	}

	// Batch Manager defaults
	if c.BatchManager.MaxBatchSize == 0 {
		c.BatchManager.MaxBatchSize = DefaultManagerConfig().MaxBatchSize
	}
	if c.BatchManager.BatchTimeout == 0 {
		c.BatchManager.BatchTimeout = DefaultManagerConfig().BatchTimeout
	}

	// Pipeline defaults
	if c.Pipeline.MaxConcurrentJobs == 0 {
		c.Pipeline.MaxConcurrentJobs = DefaultPipelineConfig().MaxConcurrentJobs
	}
	if c.Pipeline.JobTimeout == 0 {
		c.Pipeline.JobTimeout = DefaultPipelineConfig().JobTimeout
	}
	if c.Pipeline.MaxRetries == 0 {
		c.Pipeline.MaxRetries = DefaultPipelineConfig().MaxRetries
	}
	if c.Pipeline.RetryDelay == 0 {
		c.Pipeline.RetryDelay = DefaultPipelineConfig().RetryDelay
	}

	// Integration defaults (mostly boolean flags, so defaults are already set)
}

// SetChainID sets the chain ID across all relevant config sections
func (c *Config) SetChainID(chainID uint32) {
	c.BatchManager.ChainID = chainID
	c.Integration.ChainID = chainID
}

// SetL1RPC sets the L1 RPC URL for the listener
func (c *Config) SetL1RPC(rpcURL string) {
	c.L1Listener.L1RPC = rpcURL
}

// SetBatchFactor sets the L1 epoch batch factor
func (c *Config) SetBatchFactor(factor uint64) {
	c.L1Listener.BatchFactor = factor
}

// DisableBatchSync disables batch synchronization
func (c *Config) DisableBatchSync() {
	c.Enabled = false
	c.Integration.EnableBatchSync = false
}

// EnableBatchSync enables batch synchronization with validation
func (c *Config) EnableBatchSync() error {
	c.Enabled = true
	c.Integration.EnableBatchSync = true

	return c.Validate()
}

// IsProductionReady checks if the configuration is suitable for production
func (c *Config) IsProductionReady() (bool, []string) {
	if !c.Enabled {
		return false, []string{"batch sync is disabled"}
	}

	var issues []string

	// Check L1 RPC
	if c.L1Listener.L1RPC == "" ||
		c.L1Listener.L1RPC == "https://eth-mainnet.alchemyapi.io/v2/YOUR_KEY" {
		issues = append(issues, "L1 RPC URL must be configured with a real endpoint")
	}

	// Check polling interval
	if c.L1Listener.PollInterval < 10*time.Second {
		issues = append(issues, "L1 poll interval should be at least 10 seconds for production")
	}

	// Check batch timeout
	if c.BatchManager.BatchTimeout < 30*time.Minute {
		issues = append(issues, "batch timeout should be at least 30 minutes for production")
	}

	// Check job timeout
	if c.Pipeline.JobTimeout < 10*time.Minute {
		issues = append(issues, "pipeline job timeout should be at least 10 minutes for production")
	}

	// Check concurrent jobs
	if c.Pipeline.MaxConcurrentJobs > 20 {
		issues = append(issues, "max concurrent jobs should not exceed 20 to avoid resource exhaustion")
	}

	return len(issues) == 0, issues
}

// GetRecommendedProductionConfig returns a production-ready configuration
func GetRecommendedProductionConfig(chainID uint32, l1RPC string) Config {
	cfg := Config{
		Enabled: true,

		L1Listener: ListenerConfig{
			L1RPC:        l1RPC,
			BatchFactor:  10,               // Every 10 Ethereum epochs
			PollInterval: 12 * time.Second, // Match Ethereum slot time
		},

		BatchManager: ManagerConfig{
			ChainID:      chainID,
			MaxBatchSize: 320,              // ~64 minutes of blocks
			BatchTimeout: 90 * time.Minute, // Allow time for proof generation
		},

		Pipeline: PipelineConfig{
			MaxConcurrentJobs: 5,                // Conservative for production
			JobTimeout:        30 * time.Minute, // Generous for proof generation
			MaxRetries:        3,
			RetryDelay:        5 * time.Minute,
		},

		Integration: IntegrationConfig{
			ChainID:         chainID,
			EnableBatchSync: true,
			BlockReporting:  true,
		},
	}

	return cfg
}

// GetTestConfig returns a configuration suitable for testing
func GetTestConfig(chainID uint32) Config {
	cfg := Config{
		Enabled: true,

		L1Listener: ListenerConfig{
			L1RPC:        "http://localhost:8545", // Local test node
			BatchFactor:  2,                       // Faster batching for tests
			PollInterval: 2 * time.Second,         // Faster polling
		},

		BatchManager: ManagerConfig{
			ChainID:      chainID,
			MaxBatchSize: 10,              // Small batches for tests
			BatchTimeout: 5 * time.Minute, // Quick timeout
		},

		Pipeline: PipelineConfig{
			MaxConcurrentJobs: 2,
			JobTimeout:        2 * time.Minute, // Quick for tests
			MaxRetries:        1,               // Fewer retries
			RetryDelay:        10 * time.Second,
		},

		Integration: IntegrationConfig{
			ChainID:         chainID,
			EnableBatchSync: true,
			BlockReporting:  true,
		},
	}

	return cfg
}
