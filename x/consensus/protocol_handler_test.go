package consensus

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pb "github.com/ssvlabs/rollup-shared-publisher/proto/rollup/v1"
)

// Mock Coordinator for handler tests
type mockCoordinator struct {
	mock.Mock
}

func (m *mockCoordinator) StartTransaction(ctx context.Context, from string, xtReq *pb.XTRequest) error {
	args := m.Called(ctx, from, xtReq)
	return args.Error(0)
}

func (m *mockCoordinator) RecordVote(xtID *pb.XtID, chainID string, vote bool) (DecisionState, error) {
	args := m.Called(xtID, chainID, vote)
	return args.Get(0).(DecisionState), args.Error(1)
}

func (m *mockCoordinator) RecordDecision(xtID *pb.XtID, decision bool) error {
	args := m.Called(xtID, decision)
	return args.Error(0)
}

func (m *mockCoordinator) RecordCIRCMessage(circMessage *pb.CIRCMessage) error {
	args := m.Called(circMessage)
	return args.Error(0)
}

// Implement remaining Coordinator interface methods
func (m *mockCoordinator) GetTransactionState(xtID *pb.XtID) (DecisionState, error) {
	args := m.Called(xtID)
	return args.Get(0).(DecisionState), args.Error(1)
}

func (m *mockCoordinator) GetActiveTransactions() []*pb.XtID {
	args := m.Called()
	return args.Get(0).([]*pb.XtID)
}

func (m *mockCoordinator) GetState(xtID *pb.XtID) (*TwoPCState, bool) {
	args := m.Called(xtID)
	return args.Get(0).(*TwoPCState), args.Bool(1)
}

func (m *mockCoordinator) ConsumeCIRCMessage(xtID *pb.XtID, sourceChainID string) (*pb.CIRCMessage, error) {
	args := m.Called(xtID, sourceChainID)
	return args.Get(0).(*pb.CIRCMessage), args.Error(1)
}

func (m *mockCoordinator) SetStartCallback(fn StartFn) {
	m.Called(fn)
}

func (m *mockCoordinator) SetVoteCallback(fn VoteFn) {
	m.Called(fn)
}

func (m *mockCoordinator) SetDecisionCallback(fn DecisionFn) {
	m.Called(fn)
}

func (m *mockCoordinator) SetBlockCallback(fn BlockFn) {
	m.Called(fn)
}

func (m *mockCoordinator) Shutdown() error {
	args := m.Called()
	return args.Error(0)
}

func (m *mockCoordinator) OnBlockCommitted(ctx context.Context, block *types.Block) error {
	args := m.Called(ctx, block)
	return args.Error(0)
}

func (m *mockCoordinator) OnL2BlockCommitted(ctx context.Context, block *pb.L2Block) error {
	args := m.Called(ctx, block)
	return args.Error(0)
}

func TestProtocolHandler_CanHandle(t *testing.T) {
	t.Parallel()
	handler := NewProtocolHandler(nil, zerolog.Nop())

	tests := []struct {
		name     string
		msg      *pb.Message
		expected bool
	}{
		{"XTRequest", &pb.Message{Payload: &pb.Message_XtRequest{}}, true},
		{"Vote", &pb.Message{Payload: &pb.Message_Vote{}}, true},
		{"Decided", &pb.Message{Payload: &pb.Message_Decided{}}, true},
		{"CIRCMessage", &pb.Message{Payload: &pb.Message_CircMessage{}}, true},
		{"Other Message", &pb.Message{Payload: &pb.Message_StartSlot{}}, false},
		{"Nil Payload", &pb.Message{}, false},
		{"Nil Message", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, handler.CanHandle(tt.msg))
		})
	}
}

func TestProtocolHandler_Handle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	from := "test-peer"

	t.Run("XTRequest", func(t *testing.T) {
		coord := new(mockCoordinator)
		handler := NewProtocolHandler(coord, zerolog.Nop())
		xtReq := &pb.XTRequest{}
		msg := &pb.Message{Payload: &pb.Message_XtRequest{XtRequest: xtReq}}

		coord.On("StartTransaction", ctx, from, xtReq).Return(nil)
		err := handler.Handle(ctx, from, msg)
		require.NoError(t, err)
		coord.AssertExpectations(t)
	})

	t.Run("Vote", func(t *testing.T) {
		coord := new(mockCoordinator)
		handler := NewProtocolHandler(coord, zerolog.Nop())
		vote := &pb.Vote{SenderChainId: new(big.Int).SetUint64(1).Bytes(), Vote: true}
		msg := &pb.Message{Payload: &pb.Message_Vote{Vote: vote}}

		coord.On("RecordVote", vote.XtId, ChainKeyUint64(1), true).Return(StateUndecided, nil)
		err := handler.Handle(ctx, from, msg)
		require.NoError(t, err)
		coord.AssertExpectations(t)
	})

	t.Run("Decided", func(t *testing.T) {
		coord := new(mockCoordinator)
		handler := NewProtocolHandler(coord, zerolog.Nop())
		decided := &pb.Decided{Decision: true}
		msg := &pb.Message{Payload: &pb.Message_Decided{Decided: decided}}

		coord.On("RecordDecision", decided.XtId, true).Return(nil)
		err := handler.Handle(ctx, from, msg)
		require.NoError(t, err)
		coord.AssertExpectations(t)
	})

	t.Run("CIRCMessage", func(t *testing.T) {
		coord := new(mockCoordinator)
		handler := NewProtocolHandler(coord, zerolog.Nop())
		circMsg := &pb.CIRCMessage{}
		msg := &pb.Message{Payload: &pb.Message_CircMessage{CircMessage: circMsg}}

		coord.On("RecordCIRCMessage", circMsg).Return(nil)
		err := handler.Handle(ctx, from, msg)
		require.NoError(t, err)
		coord.AssertExpectations(t)
	})

	t.Run("Unknown Message", func(t *testing.T) {
		handler := NewProtocolHandler(nil, zerolog.Nop())
		msg := &pb.Message{Payload: &pb.Message_StartSlot{}}
		err := handler.Handle(ctx, from, msg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown SCP message type")
	})

	t.Run("Coordinator Error", func(t *testing.T) {
		coord := new(mockCoordinator)
		handler := NewProtocolHandler(coord, zerolog.Nop())
		xtReq := &pb.XTRequest{}
		msg := &pb.Message{Payload: &pb.Message_XtRequest{XtRequest: xtReq}}
		expectedErr := errors.New("boom")

		coord.On("StartTransaction", ctx, from, xtReq).Return(expectedErr)
		err := handler.Handle(ctx, from, msg)
		require.Error(t, err)
		assert.Equal(t, expectedErr, err)
		coord.AssertExpectations(t)
	})
}
