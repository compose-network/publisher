package mempool

import (
	"encoding/hex"
	"testing"

	pb "github.com/compose-network/publisher/proto/rollup/v1"
	"github.com/stretchr/testify/assert"
)

func TestFilter_Apply_NotSubmissionState(t *testing.T) {
	filter := NewFilter()

	records := []*Record{
		{Hash: "hash1", XtID: "xt1", Kind: KindOriginal},
		{Hash: "hash2", XtID: "xt2", Kind: KindOriginal},
	}

	// In BuildingFree state, all transactions pass through
	filtered := filter.Apply(records, StateBuildingFree, nil)
	assert.Len(t, filtered, 2)

	// In BuildingLocked state, all transactions pass through
	filtered = filter.Apply(records, StateBuildingLocked, nil)
	assert.Len(t, filtered, 2)
}

func TestFilter_Apply_SubmissionState_NoRequestSeal(t *testing.T) {
	filter := NewFilter()

	records := []*Record{
		{Hash: "hash1", XtID: "xt1", Kind: KindOriginal},
		{Hash: "hash2", XtID: "xt2", Kind: KindOriginal},
	}

	// With nil RequestSeal, all transactions pass through
	filtered := filter.Apply(records, StateSubmission, nil)
	assert.Len(t, filtered, 2)

	// With empty IncludedXts, all transactions pass through
	requestSeal := &pb.RequestSeal{IncludedXts: [][]byte{}}
	filtered = filter.Apply(records, StateSubmission, requestSeal)
	assert.Len(t, filtered, 2)
}

func TestFilter_Apply_SubmissionState_WithRequestSeal(t *testing.T) {
	filter := NewFilter()

	// Use proper hex-encoded xtIDs
	xt1Hex := "a1b2c3d4"
	xt3Hex := "e5f6a7b8"

	xt1Bytes, _ := hex.DecodeString(xt1Hex)
	xt3Bytes, _ := hex.DecodeString(xt3Hex)

	records := []*Record{
		{Hash: "hash1", XtID: xt1Hex, Kind: KindOriginal},
		{Hash: "hash2", XtID: "12345678", Kind: KindOriginal},
		{Hash: "hash3", XtID: xt3Hex, Kind: KindOriginal},
	}

	requestSeal := &pb.RequestSeal{
		IncludedXts: [][]byte{xt1Bytes, xt3Bytes},
	}

	filtered := filter.Apply(records, StateSubmission, requestSeal)

	// Only xt1 and xt3 should be included
	assert.Len(t, filtered, 2)
	assert.Equal(t, "hash1", filtered[0].Hash)
	assert.Equal(t, "hash3", filtered[1].Hash)
}

func TestFilter_Apply_SubmissionState_NoXtID(t *testing.T) {
	filter := NewFilter()

	xt1Hex := "aabbccdd"
	xt1Bytes, _ := hex.DecodeString(xt1Hex)

	records := []*Record{
		{Hash: "hash1", XtID: xt1Hex, Kind: KindOriginal},
		{Hash: "hash2", XtID: "", Kind: KindOriginal}, // No xtID
		{Hash: "hash3", XtID: "11223344", Kind: KindOriginal},
	}

	requestSeal := &pb.RequestSeal{
		IncludedXts: [][]byte{xt1Bytes},
	}

	filtered := filter.Apply(records, StateSubmission, requestSeal)

	// hash1 (xt1 in list) and hash2 (no xtID) should pass
	assert.Len(t, filtered, 2)
	assert.Equal(t, "hash1", filtered[0].Hash)
	assert.Equal(t, "hash2", filtered[1].Hash)
}

func TestFilter_Apply_SubmissionState_AllFiltered(t *testing.T) {
	filter := NewFilter()

	xt1Hex := "deadbeef"
	xt1Bytes, _ := hex.DecodeString(xt1Hex)

	records := []*Record{
		{Hash: "hash1", XtID: "11111111", Kind: KindOriginal},
		{Hash: "hash2", XtID: "22222222", Kind: KindOriginal},
	}

	requestSeal := &pb.RequestSeal{
		IncludedXts: [][]byte{xt1Bytes},
	}

	filtered := filter.Apply(records, StateSubmission, requestSeal)

	// None should be included
	assert.Len(t, filtered, 0)
}
