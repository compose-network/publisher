package coordinator

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/compose-network/compose-sdk/protocol"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"
)

func newTestCoordinator(chainID uint64) *DefaultCoordinator {
	return NewCoordinator(CoordinatorConfig{
		ChainID: chainID,
		Log:     zerolog.Nop(),
	})
}

func TestCoordinatorHandleBuilderPoll_NoActiveXT(t *testing.T) {
	coord := newTestCoordinator(901)

	ctx := context.Background()
	if err := coord.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer coord.Stop(ctx)

	req := &protocol.BuilderPollRequest{
		ChainID:         901,
		BlockNumber:     100,
		FlashblockIndex: 0,
		Timestamp:       uint64(time.Now().Unix()),
	}

	resp, err := coord.HandleBuilderPoll(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	if resp.Hold {
		t.Error("expected hold=false when no active XT")
	}
	if len(resp.Txs) != 0 {
		t.Error("expected no transactions when no active XT")
	}
}

func TestCoordinatorSubmitXT(t *testing.T) {
	coord := newTestCoordinator(901)

	ctx := context.Background()
	if err := coord.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer coord.Stop(ctx)

	// Create a minimal valid RLP-encoded transaction
	// For testing, we use empty bytes (will fail decode in real use)
	txs := map[uint64][]byte{
		901: createMinimalTx(t),
		902: createMinimalTx(t),
	}

	instanceID, err := coord.SubmitXT(ctx, "xt-1", txs)
	if err != nil {
		t.Fatal(err)
	}
	if instanceID != "xt-1" {
		t.Fatalf("expected instance_id xt-1, got %s", instanceID)
	}

	// Submitting the same ID should fail
	_, err = coord.SubmitXT(ctx, "xt-1", txs)
	if err == nil {
		t.Error("expected error when submitting duplicate XT ID")
	}
}

func TestCoordinatorOnDecision_Commit(t *testing.T) {
	coord := newTestCoordinator(901)

	ctx := context.Background()
	if err := coord.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer coord.Stop(ctx)

	txs := map[uint64][]byte{
		901: createMinimalTx(t),
		902: createMinimalTx(t),
	}

	if _, err := coord.SubmitXT(ctx, "xt-1", txs); err != nil {
		t.Fatal(err)
	}

	// Record commit decision
	if err := coord.OnDecision(ctx, "xt-1", true); err != nil {
		t.Fatal(err)
	}

	// Verify the decision was recorded
	coord.mu.RLock()
	xt := coord.pending["xt-1"]
	coord.mu.RUnlock()

	if xt.Decision == nil || !*xt.Decision {
		t.Error("expected decision to be commit (true)")
	}
}

func TestCoordinatorOnDecision_Abort(t *testing.T) {
	coord := newTestCoordinator(901)

	ctx := context.Background()
	if err := coord.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer coord.Stop(ctx)

	txs := map[uint64][]byte{
		901: createMinimalTx(t),
	}

	if _, err := coord.SubmitXT(ctx, "xt-1", txs); err != nil {
		t.Fatal(err)
	}

	// Record abort decision
	if err := coord.OnDecision(ctx, "xt-1", false); err != nil {
		t.Fatal(err)
	}

	// Verify the decision was recorded
	coord.mu.RLock()
	xt := coord.pending["xt-1"]
	coord.mu.RUnlock()

	if xt.Decision == nil || *xt.Decision {
		t.Error("expected decision to be abort (false)")
	}
}

func TestCoordinatorCleanup(t *testing.T) {
	coord := newTestCoordinator(901)

	ctx := context.Background()
	if err := coord.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer coord.Stop(ctx)

	// Submit an XT
	if _, err := coord.SubmitXT(ctx, "xt-old", map[uint64][]byte{901: createMinimalTx(t)}); err != nil {
		t.Fatal(err)
	}

	// Decide on it
	if err := coord.OnDecision(ctx, "xt-old", true); err != nil {
		t.Fatal(err)
	}

	// Set DecidedAt to be old
	coord.mu.Lock()
	coord.pending["xt-old"].DecidedAt = time.Now().Add(-2 * time.Hour)
	coord.mu.Unlock()

	// Cleanup with 1 hour max age
	coord.Cleanup(time.Hour)

	// Verify it was cleaned up
	coord.mu.RLock()
	_, exists := coord.pending["xt-old"]
	coord.mu.RUnlock()

	if exists {
		t.Error("expected old XT to be cleaned up")
	}
}

// createMinimalTx creates a minimal valid RLP-encoded legacy transaction for testing.
func createMinimalTx(t *testing.T) []byte {
	t.Helper()

	// Create a minimal valid legacy transaction
	// Using go-ethereum types to create proper RLP encoding
	to := common.HexToAddress("0x0000000000000000000000000000000000000001")
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(0),
		Data:     nil,
	})

	txBytes, err := tx.MarshalBinary()
	if err != nil {
		t.Fatalf("failed to marshal test tx: %v", err)
	}
	return txBytes
}
