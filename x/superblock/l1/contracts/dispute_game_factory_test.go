package contracts

import (
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/store"
)

func TestBuildPublishCalldataComputesHashWhenMissing(t *testing.T) {
	binding, err := NewDisputeGameFactoryBinding("0x000000000000000000000000000000000000dEaD")
	if err != nil {
		t.Fatalf("binding error: %v", err)
	}

	parentBytes, _ := hex.DecodeString("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	merkleBytes, _ := hex.DecodeString("abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd")

	sb := &store.Superblock{
		Number:     5,
		Slot:       7,
		ParentHash: common.BytesToHash(parentBytes),
		MerkleRoot: common.BytesToHash(merkleBytes),
		Hash:       common.Hash{},
	}

	calldata, err := binding.BuildPublishWithProofCalldata(t.Context(), sb, []byte{0x01})
	if err != nil {
		t.Fatalf("calldata error: %v", err)
	}

	method := binding.ABI().Methods["create"]
	unpacked, err := method.Inputs.Unpack(calldata[4:])
	if err != nil {
		t.Fatalf("unpack error: %v", err)
	}

	rootClaimBytes := unpacked[1].([32]byte)
	rootClaim := common.BytesToHash(rootClaimBytes[:])
	if (rootClaim == common.Hash{}) {
		t.Fatalf("expected non-zero root claim")
	}

	header := make([]byte, 0, 8+8+common.HashLength+common.HashLength)
	numBytes := make([]byte, 8)
	slotBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(numBytes, sb.Number)
	binary.BigEndian.PutUint64(slotBytes, sb.Slot)
	header = append(header, numBytes...)
	header = append(header, slotBytes...)
	header = append(header, sb.ParentHash.Bytes()...)
	header = append(header, sb.MerkleRoot.Bytes()...)
	expected := common.BytesToHash(crypto.Keccak256(header))

	if rootClaim != expected {
		t.Fatalf("unexpected root claim: got %s want %s", rootClaim.Hex(), expected.Hex())
	}
}
