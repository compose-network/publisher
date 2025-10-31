package manager

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLocalPeriodRunnerInitialEmissionFromGenesis(t *testing.T) {
	t.Parallel()

	const epochs = 2
	period := 20 * time.Millisecond
	genesis := time.Unix(1000, 0)

	var (
		mu      sync.Mutex
		current = genesis.Add(5 * period)
	)

	now := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return current
	}
	setNow := func(t time.Time) {
		mu.Lock()
		current = t
		mu.Unlock()
	}

	events := make(chan PeriodInfo, 10)
	runner := NewLocalPeriodRunner(PeriodRunnerConfig{
		Handler: func(ctx context.Context, info PeriodInfo) error {
			events <- info
			return nil
		},
		Epochs:      epochs,
		GenesisTime: genesis,
		Now:         now,
	})
	runner.(*LocalPeriodRunner).periodDuration = period

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, runner.Start(ctx))
	defer runner.Stop(context.Background())

	select {
	case info := <-events:
		require.Equal(t, uint64(5), info.PeriodID)
		require.WithinDuration(t, genesis.Add(5*period), info.StartedAt, time.Millisecond)
		require.Equal(t, period, info.Duration)
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for initial period emission")
	}

	setNow(genesis.Add(8 * period))
	time.Sleep(period)

	for _, expectedID := range []uint64{6, 7, 8} {
		select {
		case info := <-events:
			require.Equal(t, expectedID, info.PeriodID)
			require.WithinDuration(t, genesis.Add(time.Duration(expectedID)*period), info.StartedAt, time.Millisecond)
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for catch-up emission %d", expectedID)
		}
	}
}

func TestLocalPeriodRunnerWaitsForGenesis(t *testing.T) {
	t.Parallel()

	period := 15 * time.Millisecond
	genesis := time.Unix(2000, 0)

	var (
		mu      sync.Mutex
		current = genesis.Add(-period / 2)
	)

	now := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return current
	}
	setNow := func(t time.Time) {
		mu.Lock()
		current = t
		mu.Unlock()
	}

	events := make(chan PeriodInfo, 2)
	runner := NewLocalPeriodRunner(PeriodRunnerConfig{
		Handler: func(ctx context.Context, info PeriodInfo) error {
			events <- info
			return nil
		},
		Epochs:      1,
		GenesisTime: genesis,
		Now:         now,
	})
	runner.(*LocalPeriodRunner).periodDuration = period

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, runner.Start(ctx))
	defer runner.Stop(context.Background())

	select {
	case <-events:
		t.Fatalf("unexpected period emitted before genesis")
	default:
	}

	time.Sleep(period / 4)
	select {
	case <-events:
		t.Fatalf("unexpected period emitted before advancing time to genesis")
	default:
	}

	setNow(genesis)
	time.Sleep(period)

	select {
	case info := <-events:
		require.Equal(t, uint64(0), info.PeriodID)
		require.WithinDuration(t, genesis, info.StartedAt, time.Millisecond)
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for first period at genesis")
	}
}
