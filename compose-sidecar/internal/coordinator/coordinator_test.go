package coordinator

import (
	"context"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/compose-network/compose-sdk/protocol"
	"github.com/compose-network/compose-sidecar/internal/types"
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

type simCall struct {
	tx        []byte
	overrides map[string]any
}

type recordingSimulator struct {
	calls   []simCall
	results map[string]*protocol.SimulationResult
}

func (s *recordingSimulator) Simulate(
	ctx context.Context,
	chainID uint64,
	tx []byte,
	stateOverrides map[string]interface{},
) (*protocol.SimulationResult, error) {
	return nil, nil
}

func (s *recordingSimulator) SimulateWithMailbox(
	ctx context.Context,
	chainID uint64,
	tx []byte,
	stateOverrides map[string]interface{},
	alreadySentMsgs []protocol.CrossRollupMessage,
	fulfilledDeps []protocol.CrossRollupDependency,
) (*protocol.SimulationResult, error) {
	s.calls = append(s.calls, simCall{
		tx:        append([]byte(nil), tx...),
		overrides: cloneStateOverrides(stateOverrides),
	})
	key := hex.EncodeToString(tx)
	if result, ok := s.results[key]; ok {
		return result, nil
	}
	return &protocol.SimulationResult{ChainID: chainID, Success: true}, nil
}

func findStateDiff(overrides map[string]any, addr string) map[string]string {
	if overrides == nil {
		return nil
	}
	entry, ok := overrides[addr]
	if !ok {
		return nil
	}
	entryMap, ok := entry.(map[string]any)
	if !ok {
		return nil
	}
	diff, ok := entryMap["stateDiff"]
	if !ok {
		return nil
	}
	switch t := diff.(type) {
	case map[string]string:
		return t
	case map[string]any:
		out := make(map[string]string, len(t))
		for k, v := range t {
			if s, ok := v.(string); ok {
				out[k] = s
			}
		}
		return out
	default:
		return nil
	}
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
	txs := map[uint64][][]byte{
		901: {createMinimalTx(t)},
		902: {createMinimalTx(t)},
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

	txs := map[uint64][][]byte{
		901: {createMinimalTx(t)},
		902: {createMinimalTx(t)},
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

	txs := map[uint64][][]byte{
		901: {createMinimalTx(t)},
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
	if _, err := coord.SubmitXT(ctx, "xt-old", map[uint64][][]byte{901: {createMinimalTx(t)}}); err != nil {
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

func TestBuildXTRequestPreservesTxOrder(t *testing.T) {
	txA := createMinimalTxWithNonce(t, 1)
	txB := createMinimalTxWithNonce(t, 2)
	txC := createMinimalTxWithNonce(t, 3)

	req := buildXTRequest(map[uint64][][]byte{
		2: {txA, txB},
		1: {txC},
	})

	if len(req.TransactionRequests) != 2 {
		t.Fatalf("expected 2 transaction requests, got %d", len(req.TransactionRequests))
	}
	if req.TransactionRequests[0].ChainId != 1 {
		t.Fatalf("expected first chain 1, got %d", req.TransactionRequests[0].ChainId)
	}
	if len(req.TransactionRequests[0].Transaction) != 1 || string(req.TransactionRequests[0].Transaction[0]) != string(txC) {
		t.Fatalf("unexpected tx order for chain 1")
	}
	if req.TransactionRequests[1].ChainId != 2 {
		t.Fatalf("expected second chain 2, got %d", req.TransactionRequests[1].ChainId)
	}
	if len(req.TransactionRequests[1].Transaction) != 2 {
		t.Fatalf("expected 2 txs for chain 2, got %d", len(req.TransactionRequests[1].Transaction))
	}
	if string(req.TransactionRequests[1].Transaction[0]) != string(txA) || string(req.TransactionRequests[1].Transaction[1]) != string(txB) {
		t.Fatalf("unexpected tx order for chain 2")
	}
}

func TestProcessXTSequentialLocalTxs(t *testing.T) {
	chainID := uint64(901)
	tx1 := createMinimalTxWithNonce(t, 1)
	tx2 := createMinimalTxWithNonce(t, 2)

	txObj1 := new(ethtypes.Transaction)
	if err := txObj1.UnmarshalBinary(tx1); err != nil {
		t.Fatal(err)
	}
	txObj2 := new(ethtypes.Transaction)
	if err := txObj2.UnmarshalBinary(tx2); err != nil {
		t.Fatal(err)
	}

	addr := "0x0000000000000000000000000000000000000001"
	res1 := &protocol.SimulationResult{
		ChainID: chainID,
		Success: true,
		StateOverrides: map[string]any{
			addr: map[string]any{
				"stateDiff": map[string]string{"0x01": "0x01"},
			},
		},
	}
	res2 := &protocol.SimulationResult{
		ChainID: chainID,
		Success: true,
		StateOverrides: map[string]any{
			addr: map[string]any{
				"stateDiff": map[string]string{"0x02": "0x02"},
			},
		},
	}

	sim := &recordingSimulator{
		results: map[string]*protocol.SimulationResult{
			hex.EncodeToString(tx1): res1,
			hex.EncodeToString(tx2): res2,
		},
	}

	coord := NewCoordinator(CoordinatorConfig{
		ChainID:   chainID,
		Simulator: sim,
		Log:       zerolog.Nop(),
	})

	ctx := context.Background()
	if err := coord.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer coord.Stop(ctx)

	xt := &types.PendingXT{
		ID:           "xt-1",
		InstanceID:   []byte("xt-1"),
		Transactions: map[uint64][]*ethtypes.Transaction{chainID: {txObj1, txObj2}},
		RawTxs:       map[uint64][][]byte{chainID: {tx1, tx2}},
		ChainStates: map[uint64]*protocol.ChainState{
			chainID: {
				ChainID:         chainID,
				BlockNumber:     1,
				FlashblockIndex: 1,
				StateOverrides:  nil,
			},
		},
		LockedChains: make(map[uint64]bool),
		PeerVotes:    make(map[uint64]bool),
		CreatedAt:    time.Now(),
	}

	coord.mu.Lock()
	coord.pending["xt-1"] = xt
	coord.waiters["xt-1"] = make(map[uint64]chan *protocol.BuilderPollResponse)
	coord.mu.Unlock()

	coord.processXT(ctx, "xt-1", xt)

	if len(sim.calls) != 2 {
		t.Fatalf("expected 2 simulation calls, got %d", len(sim.calls))
	}
	if string(sim.calls[0].tx) != string(tx1) || string(sim.calls[1].tx) != string(tx2) {
		t.Fatalf("simulation order mismatch")
	}

	stateDiff := findStateDiff(sim.calls[1].overrides, addr)
	if stateDiff == nil || stateDiff["0x01"] != "0x01" {
		t.Fatalf("expected second simulation to include first tx overrides")
	}

	coord.mu.RLock()
	finalOverrides := xt.StateOverrides[chainID]
	coord.mu.RUnlock()
	finalDiff := findStateDiff(finalOverrides, addr)
	if finalDiff == nil || finalDiff["0x01"] != "0x01" || finalDiff["0x02"] != "0x02" {
		t.Fatalf("expected merged overrides to include both txs")
	}
}

// createMinimalTx creates a minimal valid RLP-encoded legacy transaction for testing.
func createMinimalTx(t *testing.T) []byte {
	return createMinimalTxWithNonce(t, 0)
}

func createMinimalTxWithNonce(t *testing.T, nonce uint64) []byte {
	t.Helper()

	// Create a minimal valid legacy transaction
	// Using go-ethereum types to create proper RLP encoding
	to := common.HexToAddress("0x0000000000000000000000000000000000000001")
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
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
