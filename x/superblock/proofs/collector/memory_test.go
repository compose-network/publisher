package collector

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs"
)

func TestMemory_SubmitAndGetStatus(t *testing.T) {
	c := NewMemory(zerolog.New(io.Discard))
	sbHash := common.HexToHash("0x" + strings.Repeat("11", 32))
	sub := proofs.Submission{
		SuperblockNumber: 42,
		SuperblockHash:   sbHash,
		ChainID:          11155111,
		ProverAddress:    common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
		L1Head:           common.HexToHash("0x" + strings.Repeat("22", 32)),
		Aggregation: proofs.AggregationOutputs{
			L1Head:           common.HexToHash("0x" + strings.Repeat("22", 32)),
			L2PreRoot:        common.HexToHash("0x" + strings.Repeat("33", 32)),
			L2PostRoot:       common.HexToHash("0x" + strings.Repeat("44", 32)),
			L2BlockNumber:    1024,
			RollupConfigHash: common.HexToHash("0x" + strings.Repeat("55", 32)),
			MultiBlockVKey:   common.HexToHash("0x" + strings.Repeat("66", 32)),
			ProverAddress:    common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
		},
		L2StartBlock:    1000,
		AggVerifyingKey: []byte("vk"),
		Proof:           []byte{1, 2, 3},
		ReceivedAt:      time.Now(),
	}
	require.NoError(t, c.SubmitOpSuccinct(t.Context(), sub))

	st, err := c.GetStatus(t.Context(), sbHash)
	require.NoError(t, err)
	require.Equal(t, uint64(42), st.SuperblockNumber)
	require.Equal(t, sbHash, st.SuperblockHash)
	require.Equal(t, "collecting", st.State)
	require.Len(t, st.Received, 1)

	list, err := c.ListSubmissions(t.Context(), sbHash)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, sub.ChainID, list[0].ChainID)

	require.NoError(t, c.UpdateStatus(t.Context(), sbHash, func(st *proofs.Status) {
		st.State = "proving"
	}))
	st2, err := c.GetStatus(t.Context(), sbHash)
	require.NoError(t, err)
	require.Equal(t, "proving", st2.State)
}
