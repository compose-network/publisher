package proofs

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

const sampleHexProof = "0xa4594c5929c1336033c0f5825389dd9e6ce40adcb9d4901246a906a56e06400c765debf72b9df5dd419de268d3e" +
	"2da1e70a46195d7aa4215b3bc39b8c0de462cc02137201d3eee90c560088ef6436c6e4394fb23d27907421b7038d37dabf505af878f3" +
	"72aa8974be8b7e4fd7a1f699d9cba47fbdd22b91a7a6b67df28b05e18535d86f023f73232b4ce12d38f5ca9b69ca47ce912f8fefd3482602" +
	"e9d2374988af086080e827050fee35c3c7c501c000e7e2ca9e3e9ec2b8af42190960565b5d8a5b0e52574d0d88f1595e074fdcef2eb76c3a" +
	"efa2c6e342615d2003a7373c8cba246812e616d067b8c4e4dc45c47214353244e1a2e744ca199a626eb47b26f0cdc1782"

func TestProofBytes_UnmarshalHex(t *testing.T) {
	var p ProofBytes
	require.NoError(t, json.Unmarshal([]byte(`"`+sampleHexProof+`"`), &p))
	require.Len(t, p, 260)
	encoded, err := json.Marshal(p)
	require.NoError(t, err)
	require.JSONEq(t, `"`+sampleHexProof+`"`, string(encoded))
}

func TestProofBytes_UnmarshalBase64(t *testing.T) {
	raw, err := hexutil.Decode(sampleHexProof)
	require.NoError(t, err)
	b64 := base64.StdEncoding.EncodeToString(raw)
	var p ProofBytes
	require.NoError(t, json.Unmarshal([]byte(`"`+b64+`"`), &p))
	require.Equal(t, raw, p.Bytes())
}

func TestProofBytes_UnmarshalArray(t *testing.T) {
	var p ProofBytes
	require.NoError(t, json.Unmarshal([]byte(`[1, 2, 3]`), &p))
	require.Equal(t, []byte{1, 2, 3}, p.Bytes())
}

func TestProofBytes_UnmarshalInvalid(t *testing.T) {
	var p ProofBytes
	require.Error(t, json.Unmarshal([]byte(`true`), &p))
}

func TestProofBytes_Clone(t *testing.T) {
	var p ProofBytes
	require.NoError(t, json.Unmarshal([]byte(`"`+sampleHexProof+`"`), &p))
	clone := p.Clone()
	require.Equal(t, p.Bytes(), clone)
	if len(clone) > 0 {
		clone[0] ^= 0xff
		require.NotEqual(t, clone[0], p.Bytes()[0])
	}
}

func TestAggregationOutputsABIEncode(t *testing.T) {
	// Create a prover address (20 bytes)
	proverAddress := common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567")

	outputs := AggregationOutputs{
		L1Head:           common.HexToHash("0x" + strings.Repeat("11", 32)),
		L2PreRoot:        common.HexToHash("0x" + strings.Repeat("22", 32)),
		L2PostRoot:       common.HexToHash("0x" + strings.Repeat("33", 32)),
		L2BlockNumber:    0x1234,
		RollupConfigHash: common.HexToHash("0x" + strings.Repeat("44", 32)),
		MailboxRoot:      common.HexToHash("0x" + strings.Repeat("55", 32)),
		MultiBlockVKey:   common.HexToHash("0x" + strings.Repeat("66", 32)),
		ProverAddress:    proverAddress,
	}
	encoded := outputs.ABIEncode()
	require.Len(t, encoded, 256) // 8*32 bytes
	// l2 block number encoded big-endian in final 8 bytes of 4th field (l2BlockNumber)
	require.Equal(t, byte(0x12), encoded[3*32+30])
	require.Equal(t, byte(0x34), encoded[3*32+31])

	// Verify prover address is properly padded (last field, 20 bytes in last 20 bytes of 32-byte slot)
	expectedAddress := proverAddress.Bytes()
	actualAddress := encoded[7*32+12:] // Last field, skip first 12 bytes (padding)
	require.Equal(t, expectedAddress, actualAddress)
}

func TestPublicValueBytes_MarshalAsArray(t *testing.T) {
	// Test that PublicValueBytes marshals as array, not base64 string
	pvb := PublicValueBytes{1, 2, 3, 255, 0}

	marshaled, err := json.Marshal(pvb)
	require.NoError(t, err)

	// Should be [1,2,3,255,0] not a base64 string
	require.JSONEq(t, `[1,2,3,255,0]`, string(marshaled))

	// Test unmarshaling back
	var pvb2 PublicValueBytes
	require.NoError(t, json.Unmarshal(marshaled, &pvb2))
	require.Equal(t, pvb.Bytes(), pvb2.Bytes())

	// Test empty slice marshals as empty array, not null
	var emptyPvb PublicValueBytes
	emptyMarshaled, err := json.Marshal(emptyPvb)
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(emptyMarshaled))
}
