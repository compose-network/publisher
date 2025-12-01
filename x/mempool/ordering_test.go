package mempool

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOrdering_BuildBundles_Empty(t *testing.T) {
	ordering := NewOrdering()

	bundles := ordering.BuildBundles(nil)
	assert.Nil(t, bundles)

	bundles = ordering.BuildBundles([]*Record{})
	assert.Nil(t, bundles)
}

func TestOrdering_BuildBundles_NoXtID(t *testing.T) {
	ordering := NewOrdering()

	records := []*Record{
		{Hash: "hash1", XtID: "", Kind: KindOriginal, Nonce: 10},
		{Hash: "hash2", XtID: "", Kind: KindOriginal, Nonce: 20},
	}

	bundles := ordering.BuildBundles(records)
	assert.Len(t, bundles, 2)
	assert.Equal(t, "hash1", bundles[0].Hashes[0])
	assert.Equal(t, "hash2", bundles[1].Hashes[0])
}

func TestOrdering_BuildBundles_SingleXT(t *testing.T) {
	ordering := NewOrdering()

	records := []*Record{
		{Hash: "original1", XtID: "xt1", Kind: KindOriginal, Nonce: 20},
		{Hash: "putInbox1", XtID: "xt1", Kind: KindPutInbox, Nonce: 10},
	}

	bundles := ordering.BuildBundles(records)
	assert.Len(t, bundles, 2)

	// PutInbox should come first
	assert.Equal(t, "putInbox1", bundles[0].Hashes[0])
	assert.Equal(t, "xt1", bundles[0].XtID)

	// Original should come second
	assert.Equal(t, "original1", bundles[1].Hashes[0])
	assert.Equal(t, "xt1", bundles[1].XtID)
}

func TestOrdering_BuildBundles_MultipleXTs(t *testing.T) {
	ordering := NewOrdering()

	records := []*Record{
		{Hash: "original1", XtID: "xt1", Kind: KindOriginal, Nonce: 30},
		{Hash: "putInbox1", XtID: "xt1", Kind: KindPutInbox, Nonce: 10},
		{Hash: "original2", XtID: "xt2", Kind: KindOriginal, Nonce: 40},
		{Hash: "putInbox2", XtID: "xt2", Kind: KindPutInbox, Nonce: 20},
	}

	bundles := ordering.BuildBundles(records)
	assert.Len(t, bundles, 4)

	// All putInbox should come before all originals
	assert.Equal(t, KindPutInbox, getRecordFromBundles(records, bundles[0].Hashes[0]).Kind)
	assert.Equal(t, KindPutInbox, getRecordFromBundles(records, bundles[1].Hashes[0]).Kind)
	assert.Equal(t, KindOriginal, getRecordFromBundles(records, bundles[2].Hashes[0]).Kind)
	assert.Equal(t, KindOriginal, getRecordFromBundles(records, bundles[3].Hashes[0]).Kind)

	// PutInbox should be ordered by nonce (10 before 20)
	assert.Equal(t, "putInbox1", bundles[0].Hashes[0])
	assert.Equal(t, "putInbox2", bundles[1].Hashes[0])

	// Originals should be ordered by nonce (30 before 40)
	assert.Equal(t, "original1", bundles[2].Hashes[0])
	assert.Equal(t, "original2", bundles[3].Hashes[0])
}

func TestOrdering_BuildBundles_MixedWithAndWithoutXtID(t *testing.T) {
	ordering := NewOrdering()

	records := []*Record{
		{Hash: "standalone1", XtID: "", Kind: KindOriginal, Nonce: 5},
		{Hash: "original1", XtID: "xt1", Kind: KindOriginal, Nonce: 30},
		{Hash: "putInbox1", XtID: "xt1", Kind: KindPutInbox, Nonce: 10},
		{Hash: "standalone2", XtID: "", Kind: KindOriginal, Nonce: 15},
	}

	bundles := ordering.BuildBundles(records)

	// Standalone transactions come first, then all putInbox, then all originals
	assert.Equal(t, "standalone1", bundles[0].Hashes[0])
	assert.Equal(t, "standalone2", bundles[1].Hashes[0])
	assert.Equal(t, "putInbox1", bundles[2].Hashes[0])
	assert.Equal(t, "original1", bundles[3].Hashes[0])
}

func TestOrdering_BuildBundles_NonceOrdering(t *testing.T) {
	ordering := NewOrdering()

	records := []*Record{
		{Hash: "putInbox3", XtID: "xt3", Kind: KindPutInbox, Nonce: 30},
		{Hash: "putInbox1", XtID: "xt1", Kind: KindPutInbox, Nonce: 10},
		{Hash: "putInbox2", XtID: "xt2", Kind: KindPutInbox, Nonce: 20},
		{Hash: "original3", XtID: "xt3", Kind: KindOriginal, Nonce: 60},
		{Hash: "original1", XtID: "xt1", Kind: KindOriginal, Nonce: 40},
		{Hash: "original2", XtID: "xt2", Kind: KindOriginal, Nonce: 50},
	}

	bundles := ordering.BuildBundles(records)

	// PutInbox transactions should be ordered by nonce: 10, 20, 30
	putInboxBundles := bundles[0:3]
	assert.Equal(t, "putInbox1", putInboxBundles[0].Hashes[0])
	assert.Equal(t, uint64(10), getRecordFromBundles(records, putInboxBundles[0].Hashes[0]).Nonce)
	assert.Equal(t, "putInbox2", putInboxBundles[1].Hashes[0])
	assert.Equal(t, uint64(20), getRecordFromBundles(records, putInboxBundles[1].Hashes[0]).Nonce)
	assert.Equal(t, "putInbox3", putInboxBundles[2].Hashes[0])
	assert.Equal(t, uint64(30), getRecordFromBundles(records, putInboxBundles[2].Hashes[0]).Nonce)

	// Original transactions should be ordered by nonce: 40, 50, 60
	originalBundles := bundles[3:6]
	assert.Equal(t, "original1", originalBundles[0].Hashes[0])
	assert.Equal(t, uint64(40), getRecordFromBundles(records, originalBundles[0].Hashes[0]).Nonce)
	assert.Equal(t, "original2", originalBundles[1].Hashes[0])
	assert.Equal(t, uint64(50), getRecordFromBundles(records, originalBundles[1].Hashes[0]).Nonce)
	assert.Equal(t, "original3", originalBundles[2].Hashes[0])
	assert.Equal(t, uint64(60), getRecordFromBundles(records, originalBundles[2].Hashes[0]).Nonce)
}

func getRecordFromBundles(records []*Record, hash string) *Record {
	for _, rec := range records {
		if rec.Hash == hash {
			return rec
		}
	}
	return nil
}
