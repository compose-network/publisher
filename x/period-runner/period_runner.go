package periodrunner

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

// LocalPeriodRunner implements PeriodRunner, emitting events according to a genesis time in an Ethereum-epoch cadence.
// An event is emitted at genesis + K * periodDuration, for K = 0,1,2,...
type LocalPeriodRunner struct {
	// Log and lifecycle
	log     zerolog.Logger
	cancel  context.CancelFunc
	started bool
	// Handler
	handler PeriodCallback
	// Time management
	periodDuration time.Duration
	now            func() time.Time
	genesisTime    time.Time
}

// NewLocalPeriodRunner constructs a LocalPeriodRunner using local time.
// If config.Handler is nil, SetHandler must be called before Start.
func NewLocalPeriodRunner(cfg PeriodRunnerConfig) PeriodRunner {

	// Default Now and EpochsPerPeriod
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.EpochsPerPeriod == 0 {
		cfg.EpochsPerPeriod = DefaultEpochsPerPeriod
	}
	// Compute period duration
	periodDuration := time.Duration(cfg.EpochsPerPeriod) * time.Duration(EthSlotsPerEpoch) * EthSlotDuration

	return &LocalPeriodRunner{
		handler:        cfg.Handler,
		periodDuration: periodDuration,
		now:            cfg.Now,
		genesisTime:    cfg.GenesisTime,
		log:            cfg.Logger,
	}
}

// SetHandler sets the handler to be called whenever a new period ticks.
// It should be called before Start; otherwise Start will panic.
func (r *LocalPeriodRunner) SetHandler(handler PeriodCallback) {
	r.handler = handler
}

// Start begins emitting period events until the context is canceled or Stop is called.
func (r *LocalPeriodRunner) Start(ctx context.Context) error {
	if r.handler == nil {
		panic("manager: LocalPeriodRunner requires a handler to start")
	}

	if r.started {
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.started = true

	if r.genesisTime.IsZero() {
		r.genesisTime = r.now()
	}

	go r.run(runCtx)
	return nil
}

// Stop halts the runner.
func (r *LocalPeriodRunner) Stop(context.Context) error {
	if !r.started {
		return nil
	}

	r.started = false
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	return nil
}

// run is invoked in Start and calls the handler whenever there is a new period.
// Track lastEmitted to ensure all missed periods are emitted up to the latest one.
func (r *LocalPeriodRunner) run(ctx context.Context) {
	now := r.now()
	var lastEmitted uint64
	hasEmitted := false

	// Compute the next period start time
	var nextStart time.Time
	if now.Before(r.genesisTime) {
		nextStart = r.genesisTime
	} else {
		currentID, periodStart := r.PeriodForTime(now)
		if err := r.emit(ctx, currentID, periodStart); err != nil {
			return
		}
		lastEmitted = currentID
		hasEmitted = true
		nextStart = r.periodStart(currentID + 1)
	}

	// Set up timer for next period
	delay := nextStart.Sub(now)
	if delay < 0 {
		delay = 0
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			now = r.now()
			// Compute next period start
			if now.Before(r.genesisTime) {
				nextStart = r.genesisTime
			} else {
				// Emit events for all missed periods
				currentID, _ := r.PeriodForTime(now)
				startID := currentID
				if hasEmitted {
					startID = lastEmitted + 1
				}
				for id := startID; id <= currentID; id++ {
					start := r.periodStart(id)
					if err := r.emit(ctx, id, start); err != nil {
						return
					}
					lastEmitted = id
					hasEmitted = true
				}
				nextStart = r.periodStart(lastEmitted + 1)
			}
			// Set timer for next period
			delay = nextStart.Sub(r.now())
			if delay < 0 {
				delay = 0
			}
			timer.Reset(delay)
		}
	}
}

// emit triggers the handler with the provided PeriodInfo.
func (r *LocalPeriodRunner) emit(ctx context.Context, periodID uint64, startedAt time.Time) error {
	info := PeriodInfo{
		PeriodID:  periodID,
		StartedAt: startedAt,
		Duration:  r.periodDuration,
	}

	if err := r.handler(ctx, info); err != nil {
		r.log.Error().Err(err).Uint64("period_id", periodID).Msg("period handler returned error")
		return err
	}
	return nil
}

// PeriodForTime returns the period ID and the corresponding period start time for the given timestamp.
func (r *LocalPeriodRunner) PeriodForTime(t time.Time) (uint64, time.Time) {
	if t.Before(r.genesisTime) {
		return 0, r.genesisTime
	}

	elapsed := t.Sub(r.genesisTime)
	currentPeriod := uint64(elapsed / r.periodDuration)
	start := r.periodStart(currentPeriod)
	return currentPeriod, start
}

// periodStart returns the start time for the given period ID.
func (r *LocalPeriodRunner) periodStart(periodID uint64) time.Time {
	return r.genesisTime.Add(time.Duration(periodID) * r.periodDuration)
}
