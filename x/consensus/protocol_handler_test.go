package consensus

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
)

// Mock Coordinator for handler tests
type mockCoordinator struct {
	mock.Mock
}

func (m *mockCoordinator) StartTransaction(ctx context.Context, from string, xtReq *pb.XTRequest) error {
	args := m.Called(ctx, from, xtReq)
	return args.Error(0)
}

func (m *mockCoordinator) RecordVote(instanceID []byte, chainID compose.ChainID, vote bool) (DecisionState, error) {
	args := m.Called(instanceID, chainID, vote)
	return args.Get(0).(DecisionState), args.Error(1)
}

func (m *mockCoordinator) RecordDecision(instanceID []byte, decision bool) error {
	args := m.Called(instanceID, decision)
	return args.Error(0)
}

func (m *mockCoordinator) RecordMailboxMessage(mailboxMessage *pb.MailboxMessage) error {
	args := m.Called(mailboxMessage)
	return args.Error(0)
}

// Implement remaining Coordinator interface methods
func (m *mockCoordinator) GetTransactionState(instanceID []byte) (DecisionState, error) {
	args := m.Called(instanceID)
	return args.Get(0).(DecisionState), args.Error(1)
}

func (m *mockCoordinator) GetActiveTransactions() [][]byte {
	args := m.Called()
	return args.Get(0).([][]byte)
}

func (m *mockCoordinator) GetState(instanceID []byte) (*TwoPCState, bool) {
	args := m.Called(instanceID)
	return args.Get(0).(*TwoPCState), args.Bool(1)
}

func (m *mockCoordinator) ConsumeMailboxMessage(
	instanceID []byte, sourceChainID compose.ChainID) (*pb.MailboxMessage, error) {
	args := m.Called(instanceID, sourceChainID)
	return args.Get(0).(*pb.MailboxMessage), args.Error(1)
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

func (m *mockCoordinator) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *mockCoordinator) Stop(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *mockCoordinator) OnBlockCommitted(ctx context.Context, block *types.Block) error {
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
		{"MailboxMessage", &pb.Message{Payload: &pb.Message_MailboxMessage{}}, true},
		{"Other Message", &pb.Message{Payload: &pb.Message_StartPeriod{}}, false},
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
		vote := &pb.Vote{ChainId: 1, Vote: true, InstanceId: []byte{'i', 'd'}}
		msg := &pb.Message{Payload: &pb.Message_Vote{Vote: vote}}

		coord.On("RecordVote", []byte{'i', 'd'}, compose.ChainID(1), true).Return(StateUndecided, nil)
		err := handler.Handle(ctx, from, msg)
		require.NoError(t, err)
		coord.AssertExpectations(t)
	})

	t.Run("Decided", func(t *testing.T) {
		coord := new(mockCoordinator)
		handler := NewProtocolHandler(coord, zerolog.Nop())
		decided := &pb.Decided{Decision: true}
		msg := &pb.Message{Payload: &pb.Message_Decided{Decided: decided}}

		coord.On("RecordDecision", decided.InstanceId, true).Return(nil)
		err := handler.Handle(ctx, from, msg)
		require.NoError(t, err)
		coord.AssertExpectations(t)
	})

	t.Run("MailboxMessage", func(t *testing.T) {
		coord := new(mockCoordinator)
		handler := NewProtocolHandler(coord, zerolog.Nop())
		mailboxMsg := &pb.MailboxMessage{}
		msg := &pb.Message{Payload: &pb.Message_MailboxMessage{MailboxMessage: mailboxMsg}}

		coord.On("RecordMailboxMessage", mailboxMsg).Return(nil)
		err := handler.Handle(ctx, from, msg)
		require.NoError(t, err)
		coord.AssertExpectations(t)
	})

	t.Run("Unknown Message", func(t *testing.T) {
		handler := NewProtocolHandler(nil, zerolog.Nop())
		msg := &pb.Message{Payload: &pb.Message_StartPeriod{}}
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
