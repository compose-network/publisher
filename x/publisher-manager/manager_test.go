package publishermanager

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	periodrunner "github.com/compose-network/publisher/x/period-runner"
	sbcpcontroller "github.com/compose-network/publisher/x/sbcp-controller"
	scpsupervisor "github.com/compose-network/publisher/x/scp-instance-supervisor"
	"github.com/compose-network/publisher/x/superblock/queue"
	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestManagerStopStopsDependencies(t *testing.T) {
	pr := &stubPeriodRunner{}
	ctrl := newStubController()
	sup := newStubSupervisor()

	mgr := newTestManager(pr, ctrl, sup)

	runCtx := context.WithValue(context.Background(), ctxKey("run"), "ctx")
	require.NoError(t, mgr.Start(runCtx))
	require.NotNil(t, pr.startCtx)

	// finalize while running: expect run context
	instanceRun := compose.Instance{ID: compose.InstanceID{0x01}}
	sup.TriggerFinalize(instanceRun)

	stopCtx := context.WithValue(context.Background(), ctxKey("stop"), "ctx")
	instanceStop := compose.Instance{ID: compose.InstanceID{0x02}}
	sup.stopFinalizeInstances = append(sup.stopFinalizeInstances, instanceStop)

	require.NoError(t, mgr.Stop(stopCtx))

	// finalize after stop: expect background context
	instanceAfter := compose.Instance{ID: compose.InstanceID{0x03}}
	sup.TriggerFinalize(instanceAfter)

	require.Equal(t, 3, len(ctrl.notifyCtxs))
	require.Equal(t, pr.startCtx, ctrl.notifyCtxs[0])
	require.Equal(t, stopCtx, ctrl.notifyCtxs[1])
	require.Equal(t, context.Background(), ctrl.notifyCtxs[2])

	require.Equal(t, stopCtx, pr.stopCtx)
	require.Equal(t, stopCtx, sup.stopCtx)
	require.Equal(t, stopCtx, ctrl.stopCtx)
}

func TestManagerStopAggregatesErrors(t *testing.T) {
	pr := &stubPeriodRunner{stopErr: errors.New("period stop")}
	ctrl := newStubController()
	ctrl.stopErr = errors.New("controller stop")
	sup := newStubSupervisor()
	sup.stopErr = errors.New("supervisor stop")

	mgr := newTestManager(pr, ctrl, sup)

	require.NoError(t, mgr.Start(context.Background()))
	err := mgr.Stop(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, pr.stopErr)
	require.ErrorIs(t, err, ctrl.stopErr)
	require.ErrorIs(t, err, sup.stopErr)
}

// --- helpers and stubs ---

type ctxKey string

func newTestManager(pr *stubPeriodRunner, ctrl *stubController, sup *stubSupervisor) *publisherManager {
	logger := zerolog.New(io.Discard).Level(zerolog.Disabled)
	mgr := &publisherManager{
		logger:         logger,
		periodRunner:   pr,
		sbcpController: ctrl,
		scpSupervisor:  sup,
	}

	pr.SetHandler(func(ctx context.Context, _ periodrunner.PeriodInfo) error {
		return ctrl.OnNewPeriod(ctx)
	})
	sup.SetOnFinalizeHook(func(instance compose.Instance) {
		if err := ctrl.NotifyInstanceDecided(mgr.notificationContext(), instance); err != nil {
			mgr.logger.Error().Err(err).Msg("failed to notify sbcp about instance decision")
		}
	})

	return mgr
}

type stubPeriodRunner struct {
	mu       sync.Mutex
	handler  periodrunner.PeriodCallback
	startCtx context.Context
	stopCtx  context.Context
	startErr error
	stopErr  error
}

func (s *stubPeriodRunner) SetHandler(cb periodrunner.PeriodCallback) {
	s.mu.Lock()
	s.handler = cb
	s.mu.Unlock()
}

func (s *stubPeriodRunner) Start(ctx context.Context) error {
	s.mu.Lock()
	s.startCtx = ctx
	s.mu.Unlock()
	return s.startErr
}

func (s *stubPeriodRunner) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.stopCtx = ctx
	s.mu.Unlock()
	return s.stopErr
}

func (s *stubPeriodRunner) PeriodForTime(time.Time) (uint64, time.Time) {
	return 0, time.Time{}
}

type stubController struct {
	mu          sync.Mutex
	notifyCtxs  []context.Context
	stopCtx     context.Context
	stopErr     error
	onNewPeriod []context.Context
	notifyErr   error
}

func newStubController() *stubController { return &stubController{} }

func (s *stubController) EnqueueXTRequest(context.Context, *pb.XTRequest, string) error { return nil }

func (s *stubController) TryProcessQueue(context.Context) error { return nil }

func (s *stubController) OnNewPeriod(ctx context.Context) error {
	s.mu.Lock()
	s.onNewPeriod = append(s.onNewPeriod, ctx)
	s.mu.Unlock()
	return nil
}

func (s *stubController) NotifyInstanceDecided(ctx context.Context, instance compose.Instance) error {
	s.mu.Lock()
	s.notifyCtxs = append(s.notifyCtxs, ctx)
	s.mu.Unlock()
	return s.notifyErr
}

func (s *stubController) AdvanceSettledState(compose.SuperblockNumber, compose.SuperBlockHash) error {
	return nil
}

func (s *stubController) ProofTimeout(context.Context) {}

func (s *stubController) SetInstanceStarter(sbcpcontroller.InstanceStarter) {}

func (s *stubController) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.stopCtx = ctx
	s.mu.Unlock()
	return s.stopErr
}

type stubSupervisor struct {
	mu                    sync.Mutex
	finalizeHook          scpsupervisor.OnFinalizeHook
	stopFinalizeInstances []compose.Instance
	stopCtx               context.Context
	stopErr               error
}

func newStubSupervisor() *stubSupervisor { return &stubSupervisor{} }

func (s *stubSupervisor) StartInstance(context.Context, *queue.QueuedXTRequest, compose.Instance) error {
	return nil
}

func (s *stubSupervisor) HandleVote(context.Context, *pb.Vote) error { return nil }

func (s *stubSupervisor) History() []scpsupervisor.CompletedInstance { return nil }

func (s *stubSupervisor) SetOnFinalizeHook(h scpsupervisor.OnFinalizeHook) {
	s.mu.Lock()
	s.finalizeHook = h
	s.mu.Unlock()
}

func (s *stubSupervisor) TriggerFinalize(instance compose.Instance) {
	s.mu.Lock()
	hook := s.finalizeHook
	s.mu.Unlock()
	if hook != nil {
		hook(instance)
	}
}

func (s *stubSupervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.stopCtx = ctx
	hook := s.finalizeHook
	instances := append([]compose.Instance(nil), s.stopFinalizeInstances...)
	s.mu.Unlock()

	for _, inst := range instances {
		if hook != nil {
			hook(inst)
		}
	}

	return s.stopErr
}
