package scpsupervisor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/compose-network/publisher/x/superblock/queue"
	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/compose-network/specs/compose/scp"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// --- test doubles ---

type stubSCPInstance struct {
	mu       sync.Mutex
	instance compose.Instance
	decision compose.DecisionState
}

func newStubSCPInstance(instance compose.Instance) *stubSCPInstance {
	return &stubSCPInstance{instance: cloneInstance(instance), decision: compose.DecisionStatePending}
}

func (s *stubSCPInstance) Run() {}
func (s *stubSCPInstance) Instance() compose.Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneInstance(s.instance)
}
func (s *stubSCPInstance) DecisionState() compose.DecisionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.decision
}
func (s *stubSCPInstance) ProcessVote(_ compose.ChainID, vote bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.decision == compose.DecisionStatePending {
		if vote {
			s.decision = compose.DecisionStateAccepted
		} else {
			s.decision = compose.DecisionStateRejected
		}
	}
	return nil
}
func (s *stubSCPInstance) Timeout() error {
	s.mu.Lock()
	if s.decision == compose.DecisionStatePending {
		s.decision = compose.DecisionStateRejected
	}
	s.mu.Unlock()
	return nil
}

type stubSCPFactory struct {
	mu      sync.Mutex
	created []*stubSCPInstance
}

func (f *stubSCPFactory) New(instance compose.Instance, _ scp.PublisherNetwork, _ zerolog.Logger) (scp.PublisherInstance, error) {
	inst := newStubSCPInstance(instance)
	f.mu.Lock()
	f.created = append(f.created, inst)
	f.mu.Unlock()
	return inst, nil
}

// Narrowed interfaces for local compile-time checks without importing external packages in tests.
// Ensure stubSCPInstance implements scp.PublisherInstance
var _ scp.PublisherInstance = (*stubSCPInstance)(nil)

// noopNetwork is a no-op implementation of scp.PublisherNetwork for tests.
type noopNetwork struct{}

func (noopNetwork) SendStartInstance(compose.Instance)   {}
func (noopNetwork) SendDecided(compose.InstanceID, bool) {}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{now: t} }
func (c *fakeClock) Now() time.Time       { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Set(t time.Time)      { c.mu.Lock(); c.now = t; c.mu.Unlock() }

// --- tests ---

func TestHistoryPrunesByMax(t *testing.T) {
	t.Parallel()
	clock := newFakeClock(time.Unix(0, 0))
	f := &stubSCPFactory{}

	cfg := DefaultConfig(zerolog.Nop(), noopNetwork{})
	cfg.Factory = f.New
	cfg.Now = clock.Now
	cfg.MaxHistory = 2
	sup := New(cfg)
	sup.SetOnFinalizeHook(func(compose.Instance) {})

	ctx := context.Background()

	// helper to start+finalize
	startAndAccept := func(idByte byte) {
		var id compose.InstanceID
		id[0] = idByte
		inst := compose.Instance{ID: id}
		queued := &queue.QueuedXTRequest{SubmittedAt: clock.Now()}
		require.NoError(t, sup.StartInstance(ctx, queued, inst))
		vote := &pb.Vote{InstanceId: id[:], ChainId: 1, Vote: true}
		require.NoError(t, sup.HandleVote(ctx, vote))
	}

	startAndAccept(0xA1)
	require.Len(t, sup.History(), 1)
	startAndAccept(0xA2)
	require.Len(t, sup.History(), 2)
	startAndAccept(0xA3)
	hist := sup.History()
	require.Len(t, hist, 2)
	// Expect last two instances kept: 0xA2, 0xA3
	require.Equal(t, byte(0xA2), hist[0].Instance.ID[0])
	require.Equal(t, byte(0xA3), hist[1].Instance.ID[0])
}

func TestHistoryPrunesByRetention(t *testing.T) {
	t.Parallel()
	clock := newFakeClock(time.Unix(0, 0))
	f := &stubSCPFactory{}

	cfg := DefaultConfig(zerolog.Nop(), noopNetwork{})
	cfg.Factory = f.New
	cfg.Now = clock.Now
	cfg.HistoryRetention = 90 * time.Minute
	sup := New(cfg)
	sup.SetOnFinalizeHook(func(compose.Instance) {})

	ctx := context.Background()

	startAndAcceptAt := func(idByte byte, now time.Time) {
		clock.Set(now)
		var id compose.InstanceID
		id[0] = idByte
		inst := compose.Instance{ID: id}
		queued := &queue.QueuedXTRequest{SubmittedAt: clock.Now()}
		require.NoError(t, sup.StartInstance(ctx, queued, inst))
		vote := &pb.Vote{InstanceId: id[:], ChainId: 1, Vote: true}
		require.NoError(t, sup.HandleVote(ctx, vote))
	}

	t0 := time.Unix(0, 0)
	startAndAcceptAt(0xB1, t0)
	startAndAcceptAt(0xB2, t0.Add(45*time.Minute))
	startAndAcceptAt(0xB3, t0.Add(90*time.Minute))

	hist := sup.History()
	require.Len(t, hist, 2)
	require.Equal(t, byte(0xB2), hist[0].Instance.ID[0])
	require.Equal(t, byte(0xB3), hist[1].Instance.ID[0])
}
