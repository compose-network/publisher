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

func TestProofCollector_SubmitAndGetStatus(t *testing.T) {
	c := New(t.Context(), zerolog.New(io.Discard))
	defer c.Close()
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
			ProverAddress:    common.HexToHash("0x0123456789abcdef0123456789abcdef01234567"),
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

func TestProofCollector_GetStats(t *testing.T) {
	t.Parallel()

	c := New(t.Context(), zerolog.New(io.Discard))
	defer c.Close()

	// Test empty stats
	stats := c.GetStats()
	require.Equal(t, 0, stats["total_superblocks"])
	require.Equal(t, 0, stats["total_submissions"])

	// Add some submissions
	sbHash1 := common.HexToHash("0x" + strings.Repeat("11", 32))
	sbHash2 := common.HexToHash("0x" + strings.Repeat("22", 32))

	sub1 := proofs.Submission{
		SuperblockNumber: 42,
		SuperblockHash:   sbHash1,
		ChainID:          11155111,
		ProverAddress:    common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
		L1Head:           common.HexToHash("0x" + strings.Repeat("33", 32)),
	}

	sub2 := proofs.Submission{
		SuperblockNumber: 43,
		SuperblockHash:   sbHash2,
		ChainID:          11155111,
		ProverAddress:    common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
		L1Head:           common.HexToHash("0x" + strings.Repeat("44", 32)),
	}

	sub3 := proofs.Submission{
		SuperblockNumber: 42,
		SuperblockHash:   sbHash1,
		ChainID:          84532,
		ProverAddress:    common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
		L1Head:           common.HexToHash("0x" + strings.Repeat("55", 32)),
	}

	require.NoError(t, c.SubmitOpSuccinct(t.Context(), sub1))
	require.NoError(t, c.SubmitOpSuccinct(t.Context(), sub2))
	require.NoError(t, c.SubmitOpSuccinct(t.Context(), sub3))

	// Test updated stats
	stats = c.GetStats()
	require.Equal(t, 2, stats["total_superblocks"])
	require.Equal(t, 3, stats["total_submissions"])

	submissionsBySb := stats["submissions_by_superblock"].(map[string]int)
	require.Equal(t, 2, submissionsBySb[sbHash1.Hex()])
	require.Equal(t, 1, submissionsBySb[sbHash2.Hex()])

	statusesByState := stats["statuses_by_state"].(map[string]int)
	require.Equal(t, 2, statusesByState["collecting"])
}
