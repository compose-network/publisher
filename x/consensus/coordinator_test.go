package consensus

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/ssvlabs/rollup-shared-publisher/proto/rollup/v1"
)

// Helper to create a test coordinator with no-op metrics
func newTestCoordinator(t *testing.T, role Role, timeout time.Duration) (*coordinator, *mockCallbacks) {
	log := zerolog.Nop()
	cfg := Config{
		NodeID:  fmt.Sprintf("test-node-%s", t.Name()),
		Role:    role,
		Timeout: timeout,
	}

	coord := NewWithMetrics(log, cfg, NewNoOpMetrics()).(*coordinator)
	callbacks := &mockCallbacks{}
	coord.SetStartCallback(callbacks.Start)
	coord.SetVoteCallback(callbacks.Vote)
	coord.SetDecisionCallback(callbacks.Decision)
	coord.SetBlockCallback(callbacks.Block)

	return coord, callbacks
}

// Mock callbacks for testing
type mockCallbacks struct {
	mu          sync.Mutex
	wg          sync.WaitGroup
	starts      []*pb.XTRequest
	votes       map[string]bool
	decisions   map[string]bool
	blocks      []*types.Block
	blockXtIDs  [][]*pb.XtID
	voteErr     error
	decisionErr error
}

func (m *mockCallbacks) Start(ctx context.Context, from string, xtReq *pb.XTRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.starts = append(m.starts, xtReq)
	m.wg.Done()
	return nil
}

func (m *mockCallbacks) Vote(ctx context.Context, xtID *pb.XtID, vote bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.votes == nil {
		m.votes = make(map[string]bool)
	}
	m.votes[xtID.Hex()] = vote
	m.wg.Done()
	return m.voteErr
}

func (m *mockCallbacks) Decision(ctx context.Context, xtID *pb.XtID, decision bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.decisions == nil {
		m.decisions = make(map[string]bool)
	}
	m.decisions[xtID.Hex()] = decision
	m.wg.Done()
	return m.decisionErr
}

func (m *mockCallbacks) Block(ctx context.Context, block *types.Block, xtIDs []*pb.XtID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocks = append(m.blocks, block)
	m.blockXtIDs = append(m.blockXtIDs, xtIDs)
	m.wg.Done()
	return nil
}

func (m *mockCallbacks) getDecision(xtID *pb.XtID) (bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.decisions[xtID.Hex()]
	return d, ok
}

func (m *mockCallbacks) getVote(xtID *pb.XtID) (bool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.votes[xtID.Hex()]
	return v, ok
}

// Helper to create a sample XTRequest
func newTestXTRequest(t *testing.T, chains []uint64) (*pb.XTRequest, *pb.XtID) {
	req := &pb.XTRequest{
		Transactions: make([]*pb.TransactionRequest, len(chains)),
	}
	for i, chain := range chains {
		req.Transactions[i] = &pb.TransactionRequest{
			ChainId:     new(big.Int).SetUint64(chain).Bytes(),
			Transaction: [][]byte{[]byte(fmt.Sprintf("tx for %d", chain))},
		}
	}
	xtID, err := req.XtID()
	require.NoError(t, err)
	return req, xtID
}

func TestNewCoordinator(t *testing.T) {
	log := zerolog.Nop()
	cfg := DefaultConfig("test")
	coord := New(log, cfg)
	require.NotNil(t, coord)

	c, ok := coord.(*coordinator)
	require.True(t, ok)
	assert.Equal(t, cfg.NodeID, c.config.NodeID)
	assert.Equal(t, cfg.Role, c.config.Role)
	assert.NotNil(t, c.stateManager)
	assert.NotNil(t, c.callbackMgr)
	assert.NotNil(t, c.metrics)
}

func TestStartTransaction(t *testing.T) {
	coord, callbacks := newTestCoordinator(t, Leader, 100*time.Millisecond)
	defer func() {
		if !coord.Stopped() {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			coord.Stop(ctx)
		}
	}()

	t.Run("happy path", func(t *testing.T) {
		xtReq, xtID := newTestXTRequest(t, []uint64{1, 2})
		callbacks.wg.Add(1)
		err := coord.StartTransaction(t.Context(), "test-sequencer", xtReq)
		require.NoError(t, err)

		state, exists := coord.GetState(xtID)
		require.True(t, exists)
		assert.Equal(t, StateUndecided, state.GetDecision())
		assert.Equal(t, 2, state.GetParticipantCount())
		assert.NotNil(t, state.Timer)

		// Check callback
		callbacks.wg.Wait()
		assert.Len(t, callbacks.starts, 1)
	})

	t.Run("already exists", func(t *testing.T) {
		xtReq, _ := newTestXTRequest(t, []uint64{3})
		callbacks.wg.Add(1)
		err := coord.StartTransaction(t.Context(), "test-sequencer", xtReq)
		callbacks.wg.Wait()
		require.NoError(t, err)

		// Try to start again
		err = coord.StartTransaction(t.Context(), "test-sequencer", xtReq)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("no chains", func(t *testing.T) {
		xtReq, _ := newTestXTRequest(t, []uint64{})
		err := coord.StartTransaction(t.Context(), "test-sequencer", xtReq)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no participating chains")
	})
}

func TestRecordVote(t *testing.T) {
	coord, callbacks := newTestCoordinator(t, Leader, 100*time.Millisecond)
	defer func() {
		if !coord.Stopped() {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			coord.Stop(ctx)
		}
	}()

	xtReq, xtID := newTestXTRequest(t, []uint64{1, 2})
	callbacks.wg.Add(1)
	err := coord.StartTransaction(t.Context(), "test-sequencer", xtReq)
	callbacks.wg.Wait()
	require.NoError(t, err)

	t.Run("valid vote", func(t *testing.T) {
		decision, err := coord.RecordVote(xtID, ChainKeyUint64(1), true)
		require.NoError(t, err)
		assert.Equal(t, StateUndecided, decision)

		state, _ := coord.GetState(xtID)
		assert.Equal(t, 1, state.GetVoteCount())
	})

	t.Run("non-existent transaction", func(t *testing.T) {
		_, nonExistentID := newTestXTRequest(t, []uint64{4})
		_, err := coord.RecordVote(nonExistentID, ChainKeyUint64(4), true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("non-participant vote", func(t *testing.T) {
		_, err := coord.RecordVote(xtID, ChainKeyUint64(3), true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not participating")
	})

	t.Run("duplicate vote", func(t *testing.T) {
		_, err := coord.RecordVote(xtID, ChainKeyUint64(1), false) // already voted
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already voted")
	})
}

func TestTwoPC_Leader_Commit(t *testing.T) {
	coord, callbacks := newTestCoordinator(t, Leader, 100*time.Millisecond)
	defer func() {
		if !coord.Stopped() {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			coord.Stop(ctx)
		}
	}()

	chains := []uint64{1, 2}
	xtReq, xtID := newTestXTRequest(t, chains)
	callbacks.wg.Add(1)
	err := coord.StartTransaction(t.Context(), "test-sequencer", xtReq)
	callbacks.wg.Wait()
	require.NoError(t, err)

	// All participants vote true
	decision, err := coord.RecordVote(xtID, ChainKeyUint64(1), true)
	require.NoError(t, err)
	assert.Equal(t, StateUndecided, decision)

	callbacks.wg.Add(1)
	decision, err = coord.RecordVote(xtID, ChainKeyUint64(2), true)
	require.NoError(t, err)
	assert.Equal(t, StateCommit, decision)

	// Check final state
	finalState, err := coord.GetTransactionState(xtID)
	require.NoError(t, err)
	assert.Equal(t, StateCommit, finalState)

	// Check callback
	callbacks.wg.Wait()
	dec, ok := callbacks.getDecision(xtID)
	require.True(t, ok)
	assert.True(t, dec)
}

func TestTwoPC_Leader_Abort(t *testing.T) {
	coord, callbacks := newTestCoordinator(t, Leader, 100*time.Millisecond)
	defer func() {
		if !coord.Stopped() {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			coord.Stop(ctx)
		}
	}()

	chains := []uint64{1, 2}
	xtReq, xtID := newTestXTRequest(t, chains)
	callbacks.wg.Add(1)
	err := coord.StartTransaction(t.Context(), "test-sequencer", xtReq)
	callbacks.wg.Wait()
	require.NoError(t, err)

	// One participant votes false
	callbacks.wg.Add(1)
	decision, err := coord.RecordVote(xtID, ChainKeyUint64(1), false)
	require.NoError(t, err)
	assert.Equal(t, StateAbort, decision)

	// Check final state
	finalState, err := coord.GetTransactionState(xtID)
	require.NoError(t, err)
	assert.Equal(t, StateAbort, finalState)

	// Check callback
	callbacks.wg.Wait()
	dec, ok := callbacks.getDecision(xtID)
	require.True(t, ok)
	assert.False(t, dec)

	// Further votes should be ignored
	_, err = coord.RecordVote(xtID, ChainKeyUint64(2), true)
	require.NoError(t, err) // returns current state, no error
	state, _ := coord.GetState(xtID)
	assert.Equal(t, 1, state.GetVoteCount()) // vote count should not increase
}

func TestTwoPC_Leader_Timeout(t *testing.T) {
	coord, callbacks := newTestCoordinator(t, Leader, 50*time.Millisecond)
	defer func() {
		if !coord.Stopped() {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			coord.Stop(ctx)
		}
	}()

	chains := []uint64{1, 2}
	xtReq, xtID := newTestXTRequest(t, chains)
	callbacks.wg.Add(2) // one for start, one for timeout decision
	err := coord.StartTransaction(t.Context(), "test-sequencer", xtReq)
	require.NoError(t, err)

	// Only one participant votes
	_, err = coord.RecordVote(xtID, ChainKeyUint64(1), true)
	require.NoError(t, err)

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Check final state
	finalState, err := coord.GetTransactionState(xtID)
	require.NoError(t, err)
	assert.Equal(t, StateAbort, finalState)

	// Check callback
	callbacks.wg.Wait()
	dec, ok := callbacks.getDecision(xtID)
	require.True(t, ok)
	assert.False(t, dec)
}

func TestTwoPC_Follower(t *testing.T) {
	coord, callbacks := newTestCoordinator(t, Follower, 100*time.Millisecond)
	defer func() {
		if !coord.Stopped() {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			coord.Stop(ctx)
		}
	}()

	chains := []uint64{1, 2}
	xtReq, xtID := newTestXTRequest(t, chains)
	callbacks.wg.Add(1)
	err := coord.StartTransaction(t.Context(), "test-sequencer", xtReq)
	callbacks.wg.Wait()
	require.NoError(t, err)

	t.Run("vote does not decide", func(t *testing.T) {
		// Follower votes, but should not decide
		callbacks.wg.Add(1)
		decision, err := coord.RecordVote(xtID, ChainKeyUint64(1), true)
		require.NoError(t, err)
		assert.Equal(t, StateUndecided, decision)

		// Check vote callback
		callbacks.wg.Wait()
		vote, ok := callbacks.getVote(xtID)
		require.True(t, ok)
		assert.True(t, vote)
	})

	t.Run("record decision", func(t *testing.T) {
		// Leader sends decision
		err := coord.RecordDecision(xtID, true)
		require.NoError(t, err)

		// Check final state
		finalState, err := coord.GetTransactionState(xtID)
		require.NoError(t, err)
		assert.Equal(t, StateCommit, finalState)

		// Follower should not trigger decision callback for itself
		_, ok := callbacks.getDecision(xtID)
		assert.False(t, ok)
	})

	t.Run("record decision for unknown tx", func(t *testing.T) {
		_, unknownXtID := newTestXTRequest(t, []uint64{99})
		err := coord.RecordDecision(unknownXtID, true)
		require.NoError(t, err) // Should not error, just log
	})

	t.Run("record decision for decided tx", func(t *testing.T) {
		err := coord.RecordDecision(xtID, false) // already decided
		require.NoError(t, err)
		finalState, _ := coord.GetTransactionState(xtID)
		assert.Equal(t, StateCommit, finalState) // decision should not change
	})
}

func TestCIRCMessageHandling(t *testing.T) {
	coord, callbacks := newTestCoordinator(t, Leader, 100*time.Millisecond)
	defer func() {
		if !coord.Stopped() {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			coord.Stop(ctx)
		}
	}()

	chains := []uint64{1, 2}
	xtReq, xtID := newTestXTRequest(t, chains)
	callbacks.wg.Add(1)
	err := coord.StartTransaction(t.Context(), "test-sequencer", xtReq)
	callbacks.wg.Wait()
	require.NoError(t, err)

	circMsg := &pb.CIRCMessage{
		SourceChain: new(big.Int).SetUint64(1).Bytes(),
		XtId:        xtID,
		Data:        [][]byte{[]byte("circ data")},
	}

	t.Run("record message", func(t *testing.T) {
		err := coord.RecordCIRCMessage(circMsg)
		require.NoError(t, err)

		state, _ := coord.GetState(xtID)
		msgs, ok := state.CIRCMessages[ChainKeyUint64(1)]
		require.True(t, ok)
		require.Len(t, msgs, 1)
		assert.Equal(t, circMsg, msgs[0])
	})

	t.Run("record for non-participant", func(t *testing.T) {
		badMsg := &pb.CIRCMessage{
			SourceChain: new(big.Int).SetUint64(3).Bytes(),
			XtId:        xtID,
		}
		err := coord.RecordCIRCMessage(badMsg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not participating")
	})

	t.Run("consume message", func(t *testing.T) {
		msg, err := coord.ConsumeCIRCMessage(xtID, ChainKeyUint64(1))
		require.NoError(t, err)
		assert.Equal(t, circMsg, msg)

		state, _ := coord.GetState(xtID)
		_, ok := state.CIRCMessages[ChainKeyUint64(1)]
		assert.False(t, ok) // queue should be empty
	})

	t.Run("consume from empty queue", func(t *testing.T) {
		_, err := coord.ConsumeCIRCMessage(xtID, ChainKeyUint64(1))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no messages available")
	})
}

func TestOnBlockCommitted(t *testing.T) {
	coord, callbacks := newTestCoordinator(t, Leader, 100*time.Millisecond)
	defer func() {
		if !coord.Stopped() {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			coord.Stop(ctx)
		}
	}()

	// Committed
	xtReq1, xtID1 := newTestXTRequest(t, []uint64{1, 2})
	callbacks.wg.Add(1)
	err := coord.StartTransaction(t.Context(), "s1", xtReq1)
	callbacks.wg.Wait()
	require.NoError(t, err)
	_, err = coord.RecordVote(xtID1, ChainKeyUint64(1), true)
	require.NoError(t, err)
	callbacks.wg.Add(1)
	_, err = coord.RecordVote(xtID1, ChainKeyUint64(2), true)
	callbacks.wg.Wait()
	require.NoError(t, err) // now committed

	// Aborted
	xtReq2, xtID2 := newTestXTRequest(t, []uint64{3})
	callbacks.wg.Add(1)
	err = coord.StartTransaction(t.Context(), "s2", xtReq2)
	callbacks.wg.Wait()
	require.NoError(t, err)
	callbacks.wg.Add(1)
	_, err = coord.RecordVote(xtID2, ChainKeyUint64(3), false)
	callbacks.wg.Wait()
	require.NoError(t, err) // now aborted

	// Undecided
	xtReq3, _ := newTestXTRequest(t, []uint64{4})
	callbacks.wg.Add(1)
	err = coord.StartTransaction(t.Context(), "s3", xtReq3)
	callbacks.wg.Wait()
	require.NoError(t, err)

	block := types.NewBlock(&types.Header{Number: big.NewInt(1)}, &types.Body{}, nil, nil)
	callbacks.wg.Add(1)
	err = coord.OnBlockCommitted(t.Context(), block)
	require.NoError(t, err)

	callbacks.wg.Wait() // allow callback to run

	require.Len(t, callbacks.blocks, 1)
	assert.Equal(t, block.Hash(), callbacks.blocks[0].Hash())

	require.Len(t, callbacks.blockXtIDs, 1)
	committedIDs := callbacks.blockXtIDs[0]
	require.Len(t, committedIDs, 1)
	assert.Equal(t, xtID1.Hex(), committedIDs[0].Hex())

	err = coord.OnBlockCommitted(t.Context(), block)
	require.NoError(t, err)
	assert.Len(t, callbacks.blocks, 1) // no new block callback
}

func TestOnL2BlockCommitted(t *testing.T) {
	coord, callbacks := newTestCoordinator(t, Follower, 100*time.Millisecond)
	defer func() {
		if !coord.Stopped() {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			coord.Stop(ctx)
		}
	}()

	xtReq, xtID := newTestXTRequest(t, []uint64{1})
	callbacks.wg.Add(1)
	err := coord.StartTransaction(t.Context(), "s1", xtReq)
	callbacks.wg.Wait()
	require.NoError(t, err)
	state, _ := coord.GetState(xtID)
	state.SetDecision(StateCommit)

	l2Block := &pb.L2Block{
		Slot:        1,
		IncludedXts: [][]byte{xtID.Hash},
	}

	// Check that the xT is not marked as sent before
	coord.sentMu.Lock()
	sent := coord.sentMap[xtID.Hex()]
	coord.sentMu.Unlock()
	assert.False(t, sent)

	// Call the function
	err = coord.OnL2BlockCommitted(t.Context(), l2Block)
	require.NoError(t, err)

	// Check that the xT is marked as sent
	coord.sentMu.Lock()
	sent = coord.sentMap[xtID.Hex()]
	coord.sentMu.Unlock()
	assert.True(t, sent)
}

func TestCoordinatorLifecycle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		role     Role
		testFunc func(t *testing.T, coord *coordinator)
	}{
		{
			name: "start_and_stop_success",
			role: Leader,
			testFunc: func(t *testing.T, coord *coordinator) {
				ctx := t.Context()
				assert.False(t, coord.Stopped())

				err := coord.Start(ctx)
				require.NoError(t, err)
				assert.False(t, coord.Stopped())

				err = coord.Stop(ctx)
				require.NoError(t, err)
				assert.True(t, coord.Stopped())
			},
		},
		{
			name: "start_twice_fails",
			role: Follower,
			testFunc: func(t *testing.T, coord *coordinator) {
				ctx := t.Context()
				err := coord.Start(ctx)
				require.NoError(t, err)

				err = coord.Start(ctx)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "already started")
			},
		},
		{
			name: "stop_without_start_safe",
			role: Leader,
			testFunc: func(t *testing.T, coord *coordinator) {
				ctx := t.Context()
				err := coord.Stop(ctx)
				require.NoError(t, err)
			},
		},
		{
			name: "stop_with_timeout",
			role: Leader,
			testFunc: func(t *testing.T, coord *coordinator) {
				err := coord.Start(t.Context())
				require.NoError(t, err)

				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
				defer cancel()

				coord.wg.Add(1)
				go func() {
					defer coord.wg.Done()
					time.Sleep(100 * time.Millisecond)
				}()

				err = coord.Stop(ctx)
				assert.Error(t, err)
				assert.Equal(t, context.DeadlineExceeded, err)

				coord.wg.Wait()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			coord, _ := newTestCoordinator(t, tt.role, time.Second)
			defer func() {
				if !coord.Stopped() {
					ctx, cancel := context.WithTimeout(t.Context(), time.Second)
					defer cancel()
					coord.Stop(ctx)
				}
			}()

			tt.testFunc(t, coord)
		})
	}
}

func TestCoordinatorConcurrentLifecycle(t *testing.T) {
	t.Parallel()

	coord, _ := newTestCoordinator(t, Leader, time.Second)
	defer func() {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		coord.Stop(ctx)
	}()

	ctx := t.Context()

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	err := coord.Start(ctx)
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := coord.Start(ctx); err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	errorCount := 0
	for err := range errors {
		assert.Contains(t, err.Error(), "already started")
		errorCount++
	}
	assert.Equal(t, 5, errorCount)

	err = coord.Stop(ctx)
	require.NoError(t, err)
}
