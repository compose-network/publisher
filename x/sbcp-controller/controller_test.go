package sbcpcontroller

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	scpsupervisor "github.com/compose-network/publisher/x/scp-instance-supervisor"
	"github.com/compose-network/publisher/x/superblock/queue"
	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/compose-network/specs/compose/sbcp"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestOnNewPeriodProcessesQueue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pub := newStubPublisher()
	starter := newStubInstanceStarter()

	q := newTestQueue()

	logger := zerolog.New(io.Discard).Level(zerolog.Disabled)
	cfg := Config{
		Logger:          logger,
		Publisher:       pub,
		Queue:           q,
		InstanceStarter: starter,
		Now:             func() time.Time { return time.Unix(0, 0) },
	}

	ctrl, err := New(cfg)
	require.NoError(t, err)

	pub.setStartInstanceResults([]startInstanceResult{{instance: newInstance(0x01)}})
	pub.allowStart = false

	require.NoError(t, ctrl.EnqueueXTRequest(ctx, newTestXTRequest(1, 2), "peer"))
	size, err := q.Size(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, size)
	require.Equal(t, 1, pub.startInstanceAttempts())
	require.Zero(t, starter.calls())

	pub.allowStart = true
	require.NoError(t, ctrl.OnNewPeriod(nil))

	require.Equal(t, 2, pub.startInstanceAttempts())
	require.Equal(t, 1, starter.calls())
	size, err = q.Size(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, size)
}

func TestTryProcessQueueRequeuesOnActiveInstance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pub := newStubPublisher()
	starter := newStubInstanceStarter()

	q := newTestQueue()
	logger := zerolog.New(io.Discard).Level(zerolog.Disabled)
	cfg := Config{Logger: logger, Publisher: pub, Queue: q, InstanceStarter: starter}

	ctrl, err := New(cfg)
	require.NoError(t, err)

	pub.allowStart = true
	pub.setStartInstanceResults([]startInstanceResult{{instance: newInstance(0x02)}, {instance: newInstance(0x03)}})

	starter.setNextErrors([]error{scpsupervisor.ErrInstanceAlreadyActive, nil})

	require.NoError(t, ctrl.EnqueueXTRequest(ctx, newTestXTRequest(3, 4), "peer"))

	size, err := q.Size(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, size)
	require.Equal(t, 1, starter.calls())

	require.NoError(t, ctrl.TryProcessQueue(ctx))
	size, err = q.Size(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, size)
	require.Equal(t, 2, starter.calls())
}

func TestNotifyInstanceDecidedAdvancesQueue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pub := newStubPublisher()
	starter := newStubInstanceStarter()

	q := newTestQueue()
	logger := zerolog.New(io.Discard).Level(zerolog.Disabled)
	cfg := Config{Logger: logger, Publisher: pub, Queue: q, InstanceStarter: starter}

	ctrl, err := New(cfg)
	require.NoError(t, err)

	first := newInstance(0x10)
	second := newInstance(0x11)
	pub.setStartInstanceResults([]startInstanceResult{{instance: first}, {instance: second}})
	pub.allowStart = true

	require.NoError(t, ctrl.EnqueueXTRequest(ctx, newTestXTRequest(5, 6), "peer"))
	require.Equal(t, 1, starter.calls())

	pub.allowStart = false
	require.NoError(t, ctrl.EnqueueXTRequest(ctx, newTestXTRequest(7, 8), "peer"))
	require.Equal(t, 2, pub.startInstanceAttempts())

	pub.allowStart = true
	require.NoError(t, ctrl.NotifyInstanceDecided(ctx, first))
	require.Equal(t, 1, pub.decideInstanceCalls())
	require.Equal(t, 3, pub.startInstanceAttempts())
	require.Equal(t, 2, starter.calls())

	size, err := q.Size(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, size)
}

func TestAdvanceSettledStateAndProofTimeout(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pub := newStubPublisher()
	starter := newStubInstanceStarter()
	q := newTestQueue()
	logger := zerolog.New(io.Discard).Level(zerolog.Disabled)
	cfg := Config{Logger: logger, Publisher: pub, Queue: q, InstanceStarter: starter}

	ctrl, err := New(cfg)
	require.NoError(t, err)

	require.NoError(t, ctrl.AdvanceSettledState(42, compose.SuperBlockHash{0xAA}))
	require.Equal(t, 1, pub.advanceCalls)

	pub.allowStart = true
	pub.setStartInstanceResults([]startInstanceResult{{instance: newInstance(0x21)}, {instance: newInstance(0x22)}})
	require.NoError(t, ctrl.EnqueueXTRequest(ctx, newTestXTRequest(9, 10), "peer"))
	require.Equal(t, 1, starter.calls())

	pub.allowStart = false
	require.NoError(t, ctrl.EnqueueXTRequest(ctx, newTestXTRequest(11, 12), "peer"))

	pub.allowStart = true
	ctrl.ProofTimeout(ctx)
	require.Equal(t, 1, pub.proofTimeoutCalls)
	require.Equal(t, 3, pub.startInstanceAttempts())
	require.Equal(t, 2, starter.calls())
}

// --- helpers and stubs ---

type startInstanceResult struct {
	instance compose.Instance
	err      error
}

type stubPublisher struct {
	mu                 sync.Mutex
	allowStart         bool
	startPeriodCount   int
	decideCount        int
	proofTimeoutCalls  int
	advanceCalls       int
	startInstanceQueue []startInstanceResult
	startInstanceCount int
}

func newStubPublisher() *stubPublisher { return &stubPublisher{} }

func (s *stubPublisher) setStartInstanceResults(results []startInstanceResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startInstanceQueue = append([]startInstanceResult(nil), results...)
	s.startInstanceCount = 0
}

func (s *stubPublisher) startInstanceAttempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startInstanceCount
}

func (s *stubPublisher) startPeriodCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startPeriodCount
}

func (s *stubPublisher) decideInstanceCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.decideCount
}

func (s *stubPublisher) StartPeriod() error {
	s.mu.Lock()
	s.startPeriodCount++
	s.mu.Unlock()
	return nil
}

func (s *stubPublisher) StartInstance(req compose.XTRequest) (compose.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startInstanceCount++
	if !s.allowStart {
		return compose.Instance{}, sbcp.ErrCannotStartInstance
	}
	if len(s.startInstanceQueue) == 0 {
		return compose.Instance{}, errors.New("stub: no instances configured")
	}
	res := s.startInstanceQueue[0]
	if len(s.startInstanceQueue) > 1 {
		s.startInstanceQueue = s.startInstanceQueue[1:]
	} else {
		s.startInstanceQueue = nil
	}
	return res.instance, res.err
}

func (s *stubPublisher) DecideInstance(instance compose.Instance) error {
	s.mu.Lock()
	s.decideCount++
	s.mu.Unlock()
	return nil
}

func (s *stubPublisher) AdvanceSettledState(superblockNumber compose.SuperblockNumber, superblockHash compose.SuperBlockHash) error {
	s.mu.Lock()
	s.advanceCalls++
	s.mu.Unlock()
	return nil
}

func (s *stubPublisher) ProofTimeout() {
	s.mu.Lock()
	s.proofTimeoutCalls++
	s.mu.Unlock()
}

type stubInstanceStarter struct {
	mu    sync.Mutex
	errQ  []error
	count int
}

func newStubInstanceStarter() *stubInstanceStarter { return &stubInstanceStarter{} }

func (s *stubInstanceStarter) setNextErrors(errs []error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errQ = append([]error(nil), errs...)
	s.count = 0
}

func (s *stubInstanceStarter) StartInstance(ctx context.Context, queued *queue.QueuedXTRequest, instance compose.Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	if len(s.errQ) == 0 {
		return nil
	}
	err := s.errQ[0]
	if len(s.errQ) > 1 {
		s.errQ = s.errQ[1:]
	}
	return err
}

var _ InstanceStarter = (*stubInstanceStarter)(nil)

func (s *stubInstanceStarter) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func newTestXTRequest(chainIDs ...uint64) *pb.XTRequest {
	req := &pb.XTRequest{TransactionRequests: make([]*pb.TransactionRequest, 0, len(chainIDs))}
	for _, id := range chainIDs {
		req.TransactionRequests = append(req.TransactionRequests, &pb.TransactionRequest{ChainId: id, Transaction: [][]byte{[]byte{byte(id)}}})
	}
	return req
}

func newInstance(idByte byte) compose.Instance {
	var id compose.InstanceID
	id[0] = idByte
	return compose.Instance{ID: id}
}

func newTestQueue() queue.XTRequestQueue {
	cfg := queue.DefaultConfig()
	cfg.RequestExpiration = 0
	return queue.NewMemoryXTRequestQueue(cfg)
}
