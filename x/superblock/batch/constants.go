package batch

import "time"

// Default configuration values for batch processing
const (
	// DefaultMaxBatchSize is the default maximum number of blocks per batch
	// Set to match SlotsPerBatch (320 slots = 10 epochs)
	DefaultMaxBatchSize = SlotsPerBatch

	// DefaultBatchTimeout is the default timeout for batch completion
	// Set to 90 minutes to allow generous time for proof generation (~1.4 batch periods)
	DefaultBatchTimeout = 90 * time.Minute

	// DefaultMaxConcurrentJobs is the default maximum number of concurrent proof jobs
	DefaultMaxConcurrentJobs = 5

	// DefaultJobTimeout is the default timeout for a single proof job
	// Includes time for range proof + aggregation proof + network aggregation
	DefaultJobTimeout = 30 * time.Minute

	// DefaultMaxRetries is the default maximum number of retry attempts for failed jobs
	DefaultMaxRetries = 3

	// DefaultRetryDelay is the default delay between retry attempts
	DefaultRetryDelay = 5 * time.Minute

	// DefaultWorkerPollInterval is the default interval for workers to check for new jobs
	DefaultWorkerPollInterval = 10 * time.Second
)

// Default channel buffer sizes
const (
	// DefaultEpochEventChannelSize is the default buffer size for epoch event channels
	DefaultEpochEventChannelSize = 10

	// DefaultBatchTriggerChannelSize is the default buffer size for batch trigger channels
	DefaultBatchTriggerChannelSize = 10

	// DefaultErrorChannelSize is the default buffer size for error channels
	DefaultErrorChannelSize = 10

	// DefaultBatchEventChannelSize is the default buffer size for batch lifecycle event channels
	DefaultBatchEventChannelSize = 100
)
