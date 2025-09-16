package http

// Route patterns for the proofs HTTP surface.
const (
	routeSubmitOpSuccinct = "/v1/proofs/op-succinct"
	routeStatusByHash     = "/v1/proofs/status/{sbHash}"
)

// Route names for mux URL building.
const (
	routeNameSubmitOpSuccinct = "proofs_submit_op_succinct"
	routeNameStatusByHash     = "proofs_status_by_hash"
)
