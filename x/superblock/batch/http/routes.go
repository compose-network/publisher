package http

// Route patterns for the batch HTTP surface.
const (
	routeCurrentBatch = "/v1/batch/current"
	routeBatchStats   = "/v1/batch/stats"
	routeEpochStatus  = "/v1/batch/epoch"
	routePipelineJobs = "/v1/batch/jobs"
)

// Route names for mux URL building.
const (
	routeNameCurrentBatch = "batch_current"
	routeNameBatchStats   = "batch_stats"
	routeNameEpochStatus  = "batch_epoch_status"
	routeNamePipelineJobs = "batch_pipeline_jobs"
)
