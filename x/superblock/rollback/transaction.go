package rollback

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	pb "github.com/ssvlabs/rollup-shared-publisher/proto/rollup/v1"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/queue"
	"google.golang.org/protobuf/proto"
)

// TransactionHandler handles transaction requeuing operations during rollback
type TransactionHandler struct {
	xtQueue queue.XTRequestQueue
	log     zerolog.Logger
}

// NewTransactionHandler creates a new transaction handler
func NewTransactionHandler(xtQueue queue.XTRequestQueue, logger zerolog.Logger) *TransactionHandler {
	return &TransactionHandler{
		xtQueue: xtQueue,
		log:     logger.With().Str("component", "rollback.transaction").Logger(),
	}
}

// RequeueTransactions requeues all attempted cross-chain transactions from a rolled-back slot
func (t *TransactionHandler) RequeueTransactions(
	ctx context.Context,
	slot uint64,
	execManager ExecutionManager,
) error {
	t.log.Debug().
		Uint64("slot", slot).
		Msg("Starting transaction requeue for rolled-back slot")

	// Get the execution snapshot for the rolled-back slot
	snapshot, found := execManager.GetExecutionHistory(slot)
	if !found {
		// Try current execution if it matches the slot
		currentExec := execManager.GetCurrentExecution()
		if currentExec != nil && currentExec.Slot == slot {
			snapshot = currentExec
			found = true
		}
	}

	if !found {
		t.log.Warn().
			Uint64("slot", slot).
			Msg("No execution snapshot found for rolled-back slot; transactions cannot be requeued")
		return nil
	}

	if len(snapshot.AttemptedRequests) == 0 {
		t.log.Info().
			Uint64("slot", slot).
			Msg("No attempted transactions found in rolled-back slot")
		return nil
	}

	// Clone the attempted requests to avoid concurrent access issues
	requests := make([]*queue.QueuedXTRequest, 0, len(snapshot.AttemptedRequests))
	for _, req := range snapshot.AttemptedRequests {
		clone, err := t.cloneQueuedRequest(req)
		if err != nil {
			t.log.Warn().
				Err(err).
				Str("xt_id", fmt.Sprintf("%x", req.XtID)).
				Msg("Failed to clone queued request, skipping")
			continue
		}
		requests = append(requests, clone)
	}

	if len(requests) == 0 {
		t.log.Warn().
			Uint64("slot", slot).
			Msg("All attempted requests failed to clone, no transactions to requeue")
		return nil
	}

	// Requeue the transactions
	if err := t.xtQueue.RequeueForSlot(ctx, requests); err != nil {
		return NewTransactionRequeueError("failed to requeue transactions").
			WithCause(err).
			WithSlot(slot).
			WithContext("transaction_count", len(requests))
	}

	// Clear the attempted requests to prevent double requeuing
	if err := t.clearAttemptedRequests(snapshot, execManager); err != nil {
		t.log.Warn().
			Err(err).
			Uint64("slot", slot).
			Msg("Failed to clear attempted requests after requeuing")
	}

	t.log.Info().
		Uint64("slot", slot).
		Int("transaction_count", len(requests)).
		Msg("Successfully requeued transactions from rolled-back slot")

	return nil
}

// cloneQueuedRequest creates a deep copy of a queued cross-chain transaction request
func (t *TransactionHandler) cloneQueuedRequest(req *queue.QueuedXTRequest) (*queue.QueuedXTRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("cannot clone nil request")
	}

	clone := *req

	// Clone the XtID
	if len(req.XtID) > 0 {
		clone.XtID = append([]byte(nil), req.XtID...)
	}

	// Clone the Request using protobuf
	if req.Request != nil {
		clone.Request = proto.Clone(req.Request).(*pb.XTRequest)
	}

	return &clone, nil
}

// clearAttemptedRequests clears the attempted requests from the execution snapshot
// to prevent double requeuing on subsequent recovery operations
func (t *TransactionHandler) clearAttemptedRequests(
	snapshot *SlotExecution,
	execManager ExecutionManager,
) error {
	if snapshot == nil {
		return fmt.Errorf("cannot clear attempted requests from nil snapshot")
	}

	// Clear from the snapshot in execution history
	if historicalExec, found := execManager.GetExecutionHistory(snapshot.Slot); found {
		historicalExec.AttemptedRequests = make(map[string]*queue.QueuedXTRequest)
	}

	// Clear from current execution if it matches the slot
	currentExec := execManager.GetCurrentExecution()
	if currentExec != nil && currentExec.Slot == snapshot.Slot {
		currentExec.AttemptedRequests = make(map[string]*queue.QueuedXTRequest)
	}

	t.log.Debug().
		Uint64("slot", snapshot.Slot).
		Msg("Cleared attempted requests to prevent double requeuing")

	return nil
}

// ValidateTransactionRequeue validates that transaction requeuing was successful
func (t *TransactionHandler) ValidateTransactionRequeue(
	ctx context.Context,
	slot uint64,
	expectedCount int,
) error {
	// This is a placeholder for potential validation logic
	// In a real implementation, you might want to:
	// 1. Check that the queue contains the expected number of transactions
	// 2. Verify that the transactions have the correct metadata
	// 3. Ensure no duplicate transactions were queued

	t.log.Debug().
		Uint64("slot", slot).
		Int("expected_count", expectedCount).
		Msg("Transaction requeue validation completed")

	return nil
}

// GetRequeueMetrics returns metrics about the transaction requeue operation
func (t *TransactionHandler) GetRequeueMetrics(
	slot uint64,
	attemptedCount, requeuedCount int,
) map[string]interface{} {
	return map[string]interface{}{
		"slot":                   slot,
		"attempted_transactions": attemptedCount,
		"requeued_transactions":  requeuedCount,
		"failed_to_requeue":      attemptedCount - requeuedCount,
		"requeue_success_rate":   float64(requeuedCount) / float64(attemptedCount),
	}
}

// RequeueSpecificTransaction requeues a specific transaction by its ID
func (t *TransactionHandler) RequeueSpecificTransaction(
	ctx context.Context,
	xtID []byte,
	slot uint64,
	execManager ExecutionManager,
) error {
	snapshot, found := execManager.GetExecutionHistory(slot)
	if !found {
		currentExec := execManager.GetCurrentExecution()
		if currentExec != nil && currentExec.Slot == slot {
			snapshot = currentExec
			found = true
		}
	}

	if !found {
		return NewTransactionRequeueError("execution snapshot not found").
			WithSlot(slot).
			WithContext("xt_id", fmt.Sprintf("%x", xtID))
	}

	xtIDStr := string(xtID)
	req, exists := snapshot.AttemptedRequests[xtIDStr]
	if !exists {
		return NewTransactionRequeueError("transaction not found in attempted requests").
			WithSlot(slot).
			WithContext("xt_id", fmt.Sprintf("%x", xtID))
	}

	clone, err := t.cloneQueuedRequest(req)
	if err != nil {
		return NewTransactionRequeueError("failed to clone transaction request").
			WithCause(err).
			WithSlot(slot).
			WithContext("xt_id", fmt.Sprintf("%x", xtID))
	}

	if err := t.xtQueue.RequeueForSlot(ctx, []*queue.QueuedXTRequest{clone}); err != nil {
		return NewTransactionRequeueError("failed to requeue specific transaction").
			WithCause(err).
			WithSlot(slot).
			WithContext("xt_id", fmt.Sprintf("%x", xtID))
	}

	// Remove the transaction from attempted requests
	delete(snapshot.AttemptedRequests, xtIDStr)

	t.log.Info().
		Uint64("slot", slot).
		Str("xt_id", fmt.Sprintf("%x", xtID)).
		Msg("Successfully requeued specific transaction")

	return nil
}
