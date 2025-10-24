package cdcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	testSlot           = Slot(10)
	testSequenceNumber = SequenceNumber(20)
	testXTID           = XTId{1}
)

func chainID(id string) ChainID {
	return ChainID(id)
}

type startMessageCall struct {
	slot  Slot
	seq   SequenceNumber
	xtReq XTRequest
	xtID  XTId
}

type decisionCall struct {
	xtID     XTId
	decision bool
}

type mockMessenger struct {
	startMessages []startMessageCall
	nativeDecided []decisionCall
	decided       []decisionCall
}

func (m *mockMessenger) SendStartMessage(slot Slot, seqNumber SequenceNumber, xtReq XTRequest, xtId XTId) {
	xtReqCopy := make(XTRequest, len(xtReq))
	copy(xtReqCopy, xtReq)
	m.startMessages = append(m.startMessages, startMessageCall{
		slot:  slot,
		seq:   seqNumber,
		xtReq: xtReqCopy,
		xtID:  xtId,
	})
}

func (m *mockMessenger) SendNativeDecided(xtId XTId, decision bool) {
	m.nativeDecided = append(m.nativeDecided, decisionCall{
		xtID:     xtId,
		decision: decision,
	})
}

func (m *mockMessenger) SendDecided(xtId XTId, decision bool) {
	m.decided = append(m.decided, decisionCall{
		xtID:     xtId,
		decision: decision,
	})
}

func newTestInstance(t *testing.T, msg Messenger, erChainID ChainID, nativeChains ...ChainID) (Instance, InstanceData) {
	t.Helper()

	data := instanceDataForChains(erChainID, nativeChains)
	inst := NewInstance(msg, data, erChainID)
	return inst, data
}

func instanceDataForChains(erChainID ChainID, nativeChains []ChainID) InstanceData {
	xtReq := make(XTRequest, 0, len(nativeChains)+1)
	for _, cid := range nativeChains {
		xtReq = append(xtReq, TransactionRequest{ChainID: cid})
	}
	xtReq = append(xtReq, TransactionRequest{ChainID: erChainID})

	return InstanceData{
		Slot:           testSlot,
		SequenceNumber: testSequenceNumber,
		XTRequest:      xtReq,
		XTId:           testXTID,
	}
}

func mustInitInstance(t *testing.T, inst Instance) {
	t.Helper()
	require.NoError(t, inst.InitInstance())
}

func mustCastVote(t *testing.T, inst Instance, chainID ChainID, vote bool) {
	t.Helper()
	require.NoError(t, inst.ProcessVote(chainID, vote))
}

func TestInstanceInitInstance(t *testing.T) {
	t.Parallel()

	msg := &mockMessenger{}
	erChain := chainID("er-chain")
	natives := []ChainID{chainID("native-1"), chainID("native-2")}
	inst, data := newTestInstance(t, msg, erChain, natives...)

	require.Equal(t, DecisionResultUndecided, inst.IsDecided())

	mustInitInstance(t, inst)

	require.Len(t, msg.startMessages, 1)
	call := msg.startMessages[0]
	require.Equal(t, data.Slot, call.slot)
	require.Equal(t, data.SequenceNumber, call.seq)
	require.Equal(t, data.XTId, call.xtID)
	require.Equal(t, data.XTRequest, call.xtReq)

	err := inst.InitInstance()
	require.ErrorIs(t, err, ErrInstanceAlreadyInitialized)
	require.Len(t, msg.startMessages, 1)

	require.Equal(t, data, inst.InstanceData())
}

func TestInstanceProcessVoteBeforeInit(t *testing.T) {
	t.Parallel()

	msg := &mockMessenger{}
	inst, _ := newTestInstance(t, msg, chainID("er"), chainID("native-1"), chainID("native-2"))

	err := inst.ProcessVote(chainID("native-1"), true)
	require.ErrorIs(t, err, ErrInstanceNotWaitingForVotes)
}

func TestInstanceProcessVoteFromERChain(t *testing.T) {
	t.Parallel()

	msg := &mockMessenger{}
	erChain := chainID("er")
	inst, _ := newTestInstance(t, msg, erChain, chainID("native-1"), chainID("native-2"))
	mustInitInstance(t, inst)

	err := inst.ProcessVote(erChain, true)
	require.ErrorIs(t, err, ErrERChainCannotSendVote)
}

func TestInstanceProcessVoteUnknownChain(t *testing.T) {
	t.Parallel()

	msg := &mockMessenger{}
	inst, _ := newTestInstance(t, msg, chainID("er"), chainID("native-1"), chainID("native-2"))
	mustInitInstance(t, inst)

	err := inst.ProcessVote(chainID("unknown"), true)
	require.ErrorIs(t, err, ErrChainIDDoesNotBelongToInstance)
}

func TestInstanceProcessVoteDuplicate(t *testing.T) {
	t.Parallel()

	msg := &mockMessenger{}
	native := chainID("native-1")
	inst, _ := newTestInstance(t, msg, chainID("er"), native, chainID("native-2"))
	mustInitInstance(t, inst)

	mustCastVote(t, inst, native, true)
	err := inst.ProcessVote(native, false)
	require.ErrorIs(t, err, ErrDuplicateVote)
}

func TestInstanceProcessVoteFalseDecision(t *testing.T) {
	t.Parallel()

	msg := &mockMessenger{}
	native := chainID("native-1")
	inst, _ := newTestInstance(t, msg, chainID("er"), native, chainID("native-2"))
	mustInitInstance(t, inst)

	mustCastVote(t, inst, native, false)

	require.Len(t, msg.decided, 1)
	require.False(t, msg.decided[0].decision)
	require.Len(t, msg.nativeDecided, 1)
	require.False(t, msg.nativeDecided[0].decision)
	require.Equal(t, DecisionResultRejected, inst.IsDecided())
}

func TestInstanceProcessVoteAllTrue(t *testing.T) {
	t.Parallel()

	msg := &mockMessenger{}
	natives := []ChainID{chainID("native-1"), chainID("native-2")}
	erChain := chainID("er")
	inst, _ := newTestInstance(t, msg, erChain, natives...)
	mustInitInstance(t, inst)

	for _, cid := range natives {
		mustCastVote(t, inst, cid, true)
	}

	require.Len(t, msg.nativeDecided, 1)
	require.True(t, msg.nativeDecided[0].decision)
	require.Empty(t, msg.decided)
	require.Equal(t, DecisionResultUndecided, inst.IsDecided())
}

func TestInstanceProcessWSDecidedNonERChain(t *testing.T) {
	t.Parallel()

	msg := &mockMessenger{}
	inst, _ := newTestInstance(t, msg, chainID("er"), chainID("native-1"), chainID("native-2"))

	err := inst.ProcessWSDecided(chainID("native-1"), true)
	require.ErrorIs(t, err, ErrOnlyERChainCanSendWSDecision)
}

func TestInstanceProcessWSDecidedWhileInit(t *testing.T) {
	t.Parallel()

	msg := &mockMessenger{}
	erChain := chainID("er")
	inst, _ := newTestInstance(t, msg, erChain, chainID("native-1"), chainID("native-2"))

	err := inst.ProcessWSDecided(erChain, true)
	require.ErrorIs(t, err, ErrInstanceCantProcessWSDecision)
}

func TestInstanceProcessWSDecidedAfterDecision(t *testing.T) {
	t.Parallel()

	msg := &mockMessenger{}
	erChain := chainID("er")
	inst, _ := newTestInstance(t, msg, erChain, chainID("native-1"), chainID("native-2"))
	mustInitInstance(t, inst)

	mustCastVote(t, inst, chainID("native-1"), false)

	err := inst.ProcessWSDecided(erChain, true)
	require.ErrorIs(t, err, ErrInstanceCantProcessWSDecision)
}

func TestInstanceProcessWSDecidedFinalizes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		decision bool
		want     DecisionResult
	}{
		{name: "accept", decision: true, want: DecisionResultAccepted},
		{name: "reject", decision: false, want: DecisionResultRejected},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			msg := &mockMessenger{}
			erChain := chainID("er")
			natives := []ChainID{chainID("native-1"), chainID("native-2")}
			inst, _ := newTestInstance(t, msg, erChain, natives...)
			mustInitInstance(t, inst)

			for _, cid := range natives {
				mustCastVote(t, inst, cid, true)
			}

			require.NoError(t, inst.ProcessWSDecided(erChain, tc.decision))
			require.Len(t, msg.decided, 1)
			require.Equal(t, tc.decision, msg.decided[0].decision)
			require.Equal(t, tc.want, inst.IsDecided())
		})
	}
}

func TestInstanceTimeoutWaitingForVotes(t *testing.T) {
	t.Parallel()

	msg := &mockMessenger{}
	erChain := chainID("er")
	natives := []ChainID{chainID("native-1"), chainID("native-2")}
	inst, _ := newTestInstance(t, msg, erChain, natives...)
	mustInitInstance(t, inst)

	mustCastVote(t, inst, natives[0], true)

	require.NoError(t, inst.Timeout())
	require.Len(t, msg.decided, 1)
	require.False(t, msg.decided[0].decision)
	require.Len(t, msg.nativeDecided, 1)
	require.False(t, msg.nativeDecided[0].decision)
	require.Equal(t, DecisionResultRejected, inst.IsDecided())
}

func TestInstanceTimeoutWaitingForWS(t *testing.T) {
	t.Parallel()

	msg := &mockMessenger{}
	erChain := chainID("er")
	natives := []ChainID{chainID("native-1"), chainID("native-2")}
	inst, _ := newTestInstance(t, msg, erChain, natives...)
	mustInitInstance(t, inst)

	for _, cid := range natives {
		mustCastVote(t, inst, cid, true)
	}

	nativeDecidedBefore := len(msg.nativeDecided)
	require.Equal(t, 1, nativeDecidedBefore)

	require.NoError(t, inst.Timeout())
	require.Empty(t, msg.decided)
	require.Len(t, msg.nativeDecided, nativeDecidedBefore)
	require.Equal(t, DecisionResultUndecided, inst.IsDecided())
}

func TestInstanceTimeoutAlreadyDecidedFalse(t *testing.T) {
	t.Parallel()

	msg := &mockMessenger{}
	erChain := chainID("er")
	native := chainID("native-1")
	inst, _ := newTestInstance(t, msg, erChain, native, chainID("native-2"))
	mustInitInstance(t, inst)

	mustCastVote(t, inst, native, false)

	decidedBefore := len(msg.decided)
	nativeBefore := len(msg.nativeDecided)

	require.NoError(t, inst.Timeout())
	require.Len(t, msg.decided, decidedBefore)
	require.Len(t, msg.nativeDecided, nativeBefore)
	require.Equal(t, DecisionResultRejected, inst.IsDecided())
}

func TestInstanceTimeoutAlreadyTrue(t *testing.T) {
	t.Parallel()

	msg := &mockMessenger{}
	erChain := chainID("er")
	natives := []ChainID{chainID("native-1"), chainID("native-2")}
	inst, _ := newTestInstance(t, msg, erChain, natives...)
	mustInitInstance(t, inst)

	for _, cid := range natives {
		mustCastVote(t, inst, cid, true)
	}
	require.NoError(t, inst.ProcessWSDecided(erChain, true))

	decidedBefore := len(msg.decided)
	nativeBefore := len(msg.nativeDecided)

	require.NoError(t, inst.Timeout())
	require.Len(t, msg.decided, decidedBefore)
	require.Len(t, msg.nativeDecided, nativeBefore)
	require.Equal(t, DecisionResultAccepted, inst.IsDecided())
}
