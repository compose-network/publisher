package mempool

import (
	"context"
	"encoding/hex"
	"testing"

	pb "github.com/compose-network/publisher/proto/rollup/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_StageAndCommit(t *testing.T) {
	mgr := NewManager(DefaultConfig())
	ctx := context.Background()

	err := mgr.StageTransaction(ctx, "hash1", "xt1", KindOriginal, 10, []byte("addr1"), 1)
	require.NoError(t, err)

	stats := mgr.Stats()
	assert.Equal(t, 1, stats.Staged)
	assert.Equal(t, 0, stats.Committed)

	err = mgr.MarkCommitted(ctx, 2, 100, []string{"hash1"})
	require.NoError(t, err)

	stats = mgr.Stats()
	assert.Equal(t, 0, stats.Staged)
	assert.Equal(t, 1, stats.Committed)
}

func TestManager_GetOrderedHashesForBlock_BuildingFree(t *testing.T) {
	mgr := NewManager(DefaultConfig())
	ctx := context.Background()

	_ = mgr.StageTransaction(ctx, "original1", "xt1", KindOriginal, 20, []byte("addr1"), 1)
	_ = mgr.StageTransaction(ctx, "putInbox1", "xt1", KindPutInbox, 10, []byte("addr2"), 1)

	hashes, err := mgr.GetOrderedHashesForBlock(
		ctx,
		StateBuildingFree,
		nil,
		nil,
	)
	require.NoError(t, err)

	// PutInbox should come before original
	assert.Len(t, hashes, 2)
	assert.Equal(t, "putInbox1", hashes[0])
	assert.Equal(t, "original1", hashes[1])
}

func TestManager_GetOrderedHashesForBlock_SubmissionWithFilter(t *testing.T) {
	mgr := NewManager(DefaultConfig())
	ctx := context.Background()

	xt1Hex := "aabbccdd"
	xt2Hex := "11223344"

	_ = mgr.StageTransaction(ctx, "original1", xt1Hex, KindOriginal, 20, []byte("addr1"), 1)
	_ = mgr.StageTransaction(ctx, "putInbox1", xt1Hex, KindPutInbox, 10, []byte("addr2"), 1)
	_ = mgr.StageTransaction(ctx, "original2", xt2Hex, KindOriginal, 30, []byte("addr3"), 1)
	_ = mgr.StageTransaction(ctx, "putInbox2", xt2Hex, KindPutInbox, 15, []byte("addr4"), 1)

	xt1Bytes, _ := hex.DecodeString(xt1Hex)

	requestSeal := &pb.RequestSeal{
		IncludedXts: [][]byte{xt1Bytes},
	}

	hashes, err := mgr.GetOrderedHashesForBlock(
		ctx,
		StateSubmission,
		requestSeal,
		nil,
	)
	require.NoError(t, err)

	// Only xt1 transactions should be included
	assert.Len(t, hashes, 2)
	assert.Equal(t, "putInbox1", hashes[0])
	assert.Equal(t, "original1", hashes[1])
}

func TestManager_GetOrderedHashesForBlock_WithNonceGapPolicy(t *testing.T) {
	mgr := NewManager(DefaultConfig())
	ctx := context.Background()

	_ = mgr.StageTransaction(ctx, "hash1", "xt1", KindOriginal, 20, []byte("addr1"), 1)
	_ = mgr.StageTransaction(ctx, "hash2", "xt2", KindOriginal, 30, []byte("addr1"), 1)

	// shouldHoldTx callback that holds hash2
	shouldHold := func(hash string, nonce uint64, from []byte) bool {
		return hash == "hash2"
	}

	hashes, err := mgr.GetOrderedHashesForBlock(
		ctx,
		StateBuildingFree,
		nil,
		shouldHold,
	)
	require.NoError(t, err)

	// Only hash1 should be returned
	assert.Len(t, hashes, 1)
	assert.Equal(t, "hash1", hashes[0])
}

func TestManager_AssignXtID(t *testing.T) {
	mgr := NewManager(DefaultConfig())
	ctx := context.Background()

	err := mgr.StageTransaction(ctx, "hash1", "", KindOriginal, 10, []byte("addr1"), 1)
	require.NoError(t, err)

	err = mgr.AssignXtID("hash1", "xt1")
	require.NoError(t, err)

	records := mgr.GetRecordsByXtID("xt1")
	assert.Len(t, records, 1)
	assert.Equal(t, "hash1", records[0].Hash)
}

func TestManager_ClearByXtID(t *testing.T) {
	mgr := NewManager(DefaultConfig())
	ctx := context.Background()

	_ = mgr.StageTransaction(ctx, "hash1", "xt1", KindOriginal, 10, []byte("addr1"), 1)
	_ = mgr.StageTransaction(ctx, "hash2", "xt1", KindPutInbox, 20, []byte("addr2"), 1)
	_ = mgr.StageTransaction(ctx, "hash3", "xt2", KindOriginal, 30, []byte("addr3"), 1)

	err := mgr.ClearByXtID(ctx, "xt1")
	require.NoError(t, err)

	stats := mgr.Stats()
	assert.Equal(t, 1, stats.Staged)

	records := mgr.GetRecordsByXtID("xt1")
	assert.Len(t, records, 0)

	records = mgr.GetRecordsByXtID("xt2")
	assert.Len(t, records, 1)
}

func TestManager_PruneGapped(t *testing.T) {
	config := Config{
		MaxFutureNonceGap:   1,
		NonceGapExpirySlots: 3,
	}
	mgr := NewManager(config)
	ctx := context.Background()

	// Transaction created at slot 1
	_ = mgr.StageTransaction(ctx, "hash1", "xt1", KindOriginal, 10, []byte("addr1"), 1)
	// Transaction created at slot 5
	_ = mgr.StageTransaction(ctx, "hash2", "xt2", KindOriginal, 20, []byte("addr2"), 5)

	// At slot 6, hash1 age = 5 >= 3 expiry, hash2 age = 1 < 3 expiry
	err := mgr.PruneGapped(ctx, 6)
	require.NoError(t, err)

	stats := mgr.Stats()
	assert.Equal(t, 1, stats.Staged)

	// hash1 should be pruned
	records := mgr.GetRecordsByXtID("xt1")
	assert.Len(t, records, 0)

	// hash2 should remain
	records = mgr.GetRecordsByXtID("xt2")
	assert.Len(t, records, 1)
}

func TestManager_Stats(t *testing.T) {
	mgr := NewManager(DefaultConfig())
	ctx := context.Background()

	_ = mgr.StageTransaction(ctx, "hash1", "xt1", KindOriginal, 10, []byte("addr1"), 1)
	_ = mgr.StageTransaction(ctx, "hash2", "xt1", KindPutInbox, 20, []byte("addr2"), 1)
	_ = mgr.StageTransaction(ctx, "hash3", "xt2", KindOriginal, 30, []byte("addr3"), 1)

	stats := mgr.Stats()
	assert.Equal(t, 3, stats.Staged)
	assert.Equal(t, 0, stats.Committed)
	assert.Equal(t, 1, stats.PutInbox)
	assert.Equal(t, 2, stats.Original)

	_ = mgr.MarkCommitted(ctx, 2, 100, []string{"hash1", "hash2"})

	stats = mgr.Stats()
	assert.Equal(t, 1, stats.Staged)
	assert.Equal(t, 2, stats.Committed)
}

func TestManager_FullLifecycle(t *testing.T) {
	mgr := NewManager(DefaultConfig())
	ctx := context.Background()

	// Stage two cross-chain transactions
	_ = mgr.StageTransaction(ctx, "original1", "xt1", KindOriginal, 20, []byte("addr1"), 1)
	_ = mgr.StageTransaction(ctx, "putInbox1", "xt1", KindPutInbox, 10, []byte("coord"), 1)
	_ = mgr.StageTransaction(ctx, "original2", "xt2", KindOriginal, 40, []byte("addr2"), 1)
	_ = mgr.StageTransaction(ctx, "putInbox2", "xt2", KindPutInbox, 30, []byte("coord"), 1)

	// Get ordered for block building
	hashes, err := mgr.GetOrderedHashesForBlock(ctx, StateBuildingFree, nil, nil)
	require.NoError(t, err)
	assert.Len(t, hashes, 4)

	// Verify ordering: all putInbox before all originals, ordered by nonce
	assert.Equal(t, "putInbox1", hashes[0])
	assert.Equal(t, "putInbox2", hashes[1])
	assert.Equal(t, "original1", hashes[2])
	assert.Equal(t, "original2", hashes[3])

	// Mark committed
	_ = mgr.MarkCommitted(ctx, 2, 100, hashes)

	stats := mgr.Stats()
	assert.Equal(t, 0, stats.Staged)
	assert.Equal(t, 4, stats.Committed)

	// Mark delivered
	_ = mgr.MarkDelivered(ctx, hashes)

	stats = mgr.Stats()
	assert.Equal(t, 0, stats.Staged)
	assert.Equal(t, 0, stats.Committed)
}
