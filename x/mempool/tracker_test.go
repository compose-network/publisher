package mempool

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTracker_Add(t *testing.T) {
	tracker := NewTracker()

	err := tracker.Add("hash1", "xt1", KindOriginal, 10, []byte("addr1"), 1)
	require.NoError(t, err)

	rec := tracker.Get("hash1")
	require.NotNil(t, rec)
	assert.Equal(t, "hash1", rec.Hash)
	assert.Equal(t, "xt1", rec.XtID)
	assert.Equal(t, KindOriginal, rec.Kind)
	assert.Equal(t, StatusStaged, rec.Status)
	assert.Equal(t, uint64(10), rec.Nonce)
	assert.Equal(t, []byte("addr1"), rec.From)
	assert.Equal(t, uint64(1), rec.CreatedSlot)
	assert.Equal(t, uint64(1), rec.Sequence)
}

func TestTracker_AddDuplicate(t *testing.T) {
	tracker := NewTracker()

	err := tracker.Add("hash1", "xt1", KindOriginal, 10, []byte("addr1"), 1)
	require.NoError(t, err)

	err = tracker.Add("hash1", "xt1", KindOriginal, 10, []byte("addr1"), 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already tracked")
}

func TestTracker_GetByStatus(t *testing.T) {
	tracker := NewTracker()

	_ = tracker.Add("hash1", "xt1", KindOriginal, 10, []byte("addr1"), 1)
	_ = tracker.Add("hash2", "xt1", KindPutInbox, 20, []byte("addr2"), 1)

	staged := tracker.GetByStatus(StatusStaged)
	assert.Len(t, staged, 2)

	_ = tracker.MarkCommitted(2, 100, []string{"hash1"})

	staged = tracker.GetByStatus(StatusStaged)
	assert.Len(t, staged, 1)

	committed := tracker.GetByStatus(StatusCommitted)
	assert.Len(t, committed, 1)
	assert.Equal(t, "hash1", committed[0].Hash)
}

func TestTracker_GetByXtID(t *testing.T) {
	tracker := NewTracker()

	_ = tracker.Add("hash1", "xt1", KindOriginal, 10, []byte("addr1"), 1)
	_ = tracker.Add("hash2", "xt1", KindPutInbox, 20, []byte("addr2"), 1)
	_ = tracker.Add("hash3", "xt2", KindOriginal, 30, []byte("addr3"), 1)

	xt1Records := tracker.GetByXtID("xt1")
	assert.Len(t, xt1Records, 2)

	xt2Records := tracker.GetByXtID("xt2")
	assert.Len(t, xt2Records, 1)
}

func TestTracker_AssignXtID(t *testing.T) {
	tracker := NewTracker()

	_ = tracker.Add("hash1", "", KindOriginal, 10, []byte("addr1"), 1)

	err := tracker.AssignXtID("hash1", "xt1")
	require.NoError(t, err)

	rec := tracker.Get("hash1")
	assert.Equal(t, "xt1", rec.XtID)

	xt1Records := tracker.GetByXtID("xt1")
	assert.Len(t, xt1Records, 1)
}

func TestTracker_AssignXtID_NotFound(t *testing.T) {
	tracker := NewTracker()

	err := tracker.AssignXtID("nonexistent", "xt1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestTracker_MarkCommitted(t *testing.T) {
	tracker := NewTracker()

	_ = tracker.Add("hash1", "xt1", KindOriginal, 10, []byte("addr1"), 1)
	_ = tracker.Add("hash2", "xt1", KindPutInbox, 20, []byte("addr2"), 1)

	err := tracker.MarkCommitted(2, 100, []string{"hash1", "hash2"})
	require.NoError(t, err)

	rec1 := tracker.Get("hash1")
	assert.Equal(t, StatusCommitted, rec1.Status)
	assert.Equal(t, uint64(2), rec1.CommittedSlot)
	assert.Equal(t, uint64(100), rec1.CommittedBlock)

	staged := tracker.GetByStatus(StatusStaged)
	assert.Len(t, staged, 0)

	committed := tracker.GetByStatus(StatusCommitted)
	assert.Len(t, committed, 2)
}

func TestTracker_MarkDelivered(t *testing.T) {
	tracker := NewTracker()

	_ = tracker.Add("hash1", "xt1", KindOriginal, 10, []byte("addr1"), 1)
	_ = tracker.Add("hash2", "xt1", KindPutInbox, 20, []byte("addr2"), 1)
	_ = tracker.MarkCommitted(2, 100, []string{"hash1", "hash2"})

	err := tracker.MarkDelivered([]string{"hash1", "hash2"})
	require.NoError(t, err)

	assert.Nil(t, tracker.Get("hash1"))
	assert.Nil(t, tracker.Get("hash2"))

	committed := tracker.GetByStatus(StatusCommitted)
	assert.Len(t, committed, 0)

	xt1Records := tracker.GetByXtID("xt1")
	assert.Len(t, xt1Records, 0)
}

func TestTracker_ClearByXtID(t *testing.T) {
	tracker := NewTracker()

	_ = tracker.Add("hash1", "xt1", KindOriginal, 10, []byte("addr1"), 1)
	_ = tracker.Add("hash2", "xt1", KindPutInbox, 20, []byte("addr2"), 1)
	_ = tracker.Add("hash3", "xt2", KindOriginal, 30, []byte("addr3"), 1)

	err := tracker.ClearByXtID("xt1")
	require.NoError(t, err)

	assert.Nil(t, tracker.Get("hash1"))
	assert.Nil(t, tracker.Get("hash2"))
	assert.NotNil(t, tracker.Get("hash3"))

	xt1Records := tracker.GetByXtID("xt1")
	assert.Len(t, xt1Records, 0)
}

func TestTracker_Remove(t *testing.T) {
	tracker := NewTracker()

	_ = tracker.Add("hash1", "xt1", KindOriginal, 10, []byte("addr1"), 1)
	_ = tracker.Add("hash2", "xt1", KindPutInbox, 20, []byte("addr2"), 1)

	err := tracker.Remove([]string{"hash1"})
	require.NoError(t, err)

	assert.Nil(t, tracker.Get("hash1"))
	assert.NotNil(t, tracker.Get("hash2"))

	staged := tracker.GetByStatus(StatusStaged)
	assert.Len(t, staged, 1)
}

func TestTracker_All(t *testing.T) {
	tracker := NewTracker()

	_ = tracker.Add("hash1", "xt1", KindOriginal, 10, []byte("addr1"), 1)
	_ = tracker.Add("hash2", "xt1", KindPutInbox, 20, []byte("addr2"), 1)
	_ = tracker.Add("hash3", "xt2", KindOriginal, 30, []byte("addr3"), 1)

	all := tracker.All()
	assert.Len(t, all, 3)
}

func TestTracker_SequenceNumbers(t *testing.T) {
	tracker := NewTracker()

	_ = tracker.Add("hash1", "xt1", KindOriginal, 10, []byte("addr1"), 1)
	_ = tracker.Add("hash2", "xt2", KindOriginal, 20, []byte("addr2"), 1)
	_ = tracker.Add("hash3", "xt3", KindOriginal, 30, []byte("addr3"), 1)

	rec1 := tracker.Get("hash1")
	rec2 := tracker.Get("hash2")
	rec3 := tracker.Get("hash3")

	assert.Equal(t, uint64(1), rec1.Sequence)
	assert.Equal(t, uint64(2), rec2.Sequence)
	assert.Equal(t, uint64(3), rec3.Sequence)
}
