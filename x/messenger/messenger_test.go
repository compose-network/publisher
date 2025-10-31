package messenger

import (
    "context"
    "io"
    "testing"

    "github.com/compose-network/specs/compose"
    pb "github.com/compose-network/specs/compose/proto"
    "github.com/rs/zerolog"
    "github.com/stretchr/testify/require"
)

type mockBroadcaster struct {
    msg      *pb.Message
    exclude  string
    calls    int
    retError error
}

func (m *mockBroadcaster) Broadcast(ctx context.Context, msg *pb.Message, excludeID string) error {
    m.msg = msg
    m.exclude = excludeID
    m.calls++
    return m.retError
}

func newTestMessenger(b Broadcaster) Messenger {
    logger := zerolog.New(io.Discard)
    return NewMessenger(context.Background(), logger, b)
}

func TestSendStartInstance_BuildsAndBroadcastsMessage(t *testing.T) {
    mb := &mockBroadcaster{}
    m := newTestMessenger(mb)

    var id compose.InstanceID
    id[0] = 0xAA
    id[1] = 0xBB
    instance := compose.Instance{
        ID:             id,
        PeriodID:       compose.PeriodID(7),
        SequenceNumber: compose.SequenceNumber(3),
        XTRequest: compose.XTRequest{
            Transactions: []compose.TransactionRequest{
                {
                    ChainID:      compose.ChainID(1),
                    Transactions:  [][]byte{[]byte{0x01, 0x02}, []byte{0x03}},
                },
                {
                    ChainID:      compose.ChainID(2),
                    Transactions:  [][]byte{[]byte{0x0A}},
                },
            },
        },
    }

    m.SendStartInstance(instance)

    require.Equal(t, 1, mb.calls)
    require.NotNil(t, mb.msg)
    require.Equal(t, "publisher", mb.msg.SenderId)
    require.Equal(t, "", mb.exclude)

    payload, ok := mb.msg.Payload.(*pb.Message_StartInstance)
    require.True(t, ok, "payload should be StartInstance")

    si := payload.StartInstance
    require.NotNil(t, si)
    require.Equal(t, instance.ID[:], si.InstanceId)
    require.Equal(t, uint64(instance.PeriodID), si.PeriodId)
    require.Equal(t, uint64(instance.SequenceNumber), si.SequenceNumber)

    require.NotNil(t, si.XtRequest)
    require.Len(t, si.XtRequest.TransactionRequests, len(instance.XTRequest.Transactions))

    for i, tr := range instance.XTRequest.Transactions {
        got := si.XtRequest.TransactionRequests[i]
        require.Equal(t, uint64(tr.ChainID), got.ChainId)
        require.Equal(t, tr.Transactions, got.Transaction)
    }
}

func TestSendDecided_BuildsAndBroadcastsMessage(t *testing.T) {
    mb := &mockBroadcaster{}
    m := newTestMessenger(mb)

    var id compose.InstanceID
    id[0] = 0x11
    id[1] = 0x22

    m.SendDecided(id, true)

    require.Equal(t, 1, mb.calls)
    require.NotNil(t, mb.msg)
    require.Equal(t, "publisher", mb.msg.SenderId)
    require.Equal(t, "", mb.exclude)

    payload, ok := mb.msg.Payload.(*pb.Message_Decided)
    require.True(t, ok, "payload should be Decided")
    require.Equal(t, id[:], payload.Decided.InstanceId)
    require.True(t, payload.Decided.Decision)
}

func TestBroadcastStartPeriod_BuildsAndBroadcastsMessage(t *testing.T) {
    mb := &mockBroadcaster{}
    m := newTestMessenger(mb)

    periodID := compose.PeriodID(42)
    superblock := compose.SuperblockNumber(1001)

    m.BroadcastStartPeriod(periodID, superblock)

    require.Equal(t, 1, mb.calls)
    require.NotNil(t, mb.msg)
    require.Equal(t, "publisher", mb.msg.SenderId)

    payload, ok := mb.msg.Payload.(*pb.Message_StartPeriod)
    require.True(t, ok, "payload should be StartPeriod")
    require.Equal(t, uint64(periodID), payload.StartPeriod.PeriodId)
    require.Equal(t, uint64(superblock), payload.StartPeriod.SuperblockNumber)
}

func TestBroadcastRollback_BuildsAndBroadcastsMessage(t *testing.T) {
    mb := &mockBroadcaster{}
    m := newTestMessenger(mb)

    periodID := compose.PeriodID(5)
    number := compose.SuperblockNumber(77)
    var hash compose.SuperBlockHash
    hash[0] = 0xAB
    hash[1] = 0xCD

    m.BroadcastRollback(periodID, number, hash)

    require.Equal(t, 1, mb.calls)
    require.NotNil(t, mb.msg)
    require.Equal(t, "publisher", mb.msg.SenderId)

    payload, ok := mb.msg.Payload.(*pb.Message_Rollback)
    require.True(t, ok, "payload should be Rollback")
    rb := payload.Rollback
    require.Equal(t, uint64(periodID), rb.PeriodId)
    require.Equal(t, uint64(number), rb.LastFinalizedSuperblockNumber)
    require.Equal(t, hash[:], rb.LastFinalizedSuperblockHash)
}

