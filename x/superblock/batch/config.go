package batch

import (
	"fmt"
	"time"
)

// Config holds all batch synchronization configuration
//
// //nolint:lll // Config struct is long
type Config struct {
	Enabled            bool          `mapstructure:"enabled"              yaml:"enabled"`              // Enable batch synchronization
	GenesisTime        int64         `mapstructure:"genesis_time"         yaml:"genesis_time"`         // Unix timestamp (e.g., 1606824023 for Ethereum Mainnet)
	ChainID            uint32        `mapstructure:"chain_id"             yaml:"chain_id"`             // Chain ID for this rollup
	MaxConcurrentJobs  int           `mapstructure:"max_concurrent_jobs"  yaml:"max_concurrent_jobs"`  // Max concurrent proof jobs
	WorkerPollInterval time.Duration `mapstructure:"worker_poll_interval" yaml:"worker_poll_interval"` // How often workers poll for jobs
}

// DefaultConfig returns the default batch synchronization configuration
func DefaultConfig() Config {
	return Config{
		Enabled:            true,
		GenesisTime:        EthereumMainnetGenesis,
		ChainID:            0, // Must be set by user
		MaxConcurrentJobs:  DefaultMaxConcurrentJobs,
		WorkerPollInterval: DefaultWorkerPollInterval,
	}
}

// Validate checks that the configuration is valid
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.GenesisTime == 0 {
		return fmt.Errorf("genesis_time is required when batch sync is enabled")
	}

	if c.ChainID == 0 {
		return fmt.Errorf("chain_id is required when batch sync is enabled")
	}

	return nil
}

// GetEpochTrackerConfig returns epoch tracker configuration with defaults
func (c *Config) GetEpochTrackerConfig() EpochTrackerConfig {
	return EpochTrackerConfig{
		GenesisTime: c.GenesisTime,
		BatchFactor: BatchFactor,
	}
}

// GetManagerConfig returns batch manager configuration with defaults
func (c *Config) GetManagerConfig() ManagerConfig {
	return ManagerConfig{
		ChainID:      c.ChainID,
		MaxBatchSize: DefaultMaxBatchSize,
		BatchTimeout: DefaultBatchTimeout,
	}
}

// GetPipelineConfig returns pipeline configuration with defaults
func (c *Config) GetPipelineConfig() PipelineConfig {
	maxJobs := c.MaxConcurrentJobs
	if maxJobs <= 0 {
		maxJobs = DefaultMaxConcurrentJobs
	}

	return PipelineConfig{
		MaxConcurrentJobs: maxJobs,
		JobTimeout:        DefaultJobTimeout,
		MaxRetries:        DefaultMaxRetries,
		RetryDelay:        DefaultRetryDelay,
	}
}

// GetWorkerPollInterval returns the worker poll interval with default
func (c *Config) GetWorkerPollInterval() time.Duration {
	if c.WorkerPollInterval <= 0 {
		return DefaultWorkerPollInterval
	}
	return c.WorkerPollInterval
}

// GetIntegrationConfig returns integration configuration with defaults
func (c *Config) GetIntegrationConfig() IntegrationConfig {
	return IntegrationConfig{
		ChainID:         c.ChainID,
		EnableBatchSync: c.Enabled,
		BlockReporting:  c.Enabled,
	}
}
