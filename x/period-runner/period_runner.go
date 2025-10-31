package manager

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

const (
	DefaultEpochsPerPeriod = 10 // Number of Ethereum epochs that consists a periodDuration
	EthSlotsPerEpoch       = uint64(32)
	EthSlotDuration        = 12 * time.Second
)

var (
	// DefaultGenesisTime is a time.Time variable that represents 2025/10/29 00:00:00 UTC
	DefaultGenesisTime = time.Date(2025, 10, 29, 0, 0, 0, 0, time.UTC)
)

type PeriodRunnerBuilder func(cfg PeriodRunnerConfig) PeriodRunner

// PeriodRunner drives onNewPeriod notifications.
type PeriodRunner interface {
	SetHandler(PeriodCallback)
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	PeriodForTime(t time.Time) (uint64, time.Time)
}

// PeriodInfo represents an SBCP period.
type PeriodInfo struct {
	PeriodID  uint64
	StartedAt time.Time
	Duration  time.Duration
}

// PeriodCallback is a hook invoked by PeriodRunner whenever a new SBCP periodDuration starts.
type PeriodCallback func(context.Context, PeriodInfo) error

// PeriodRunnerConfig configures a PeriodRunner.
type PeriodRunnerConfig struct {
	Handler     PeriodCallback
	Epochs      uint64
	GenesisTime time.Time
	Now         func() time.Time // Defaults to time.Now if nil
	Logger      zerolog.Logger
}

func DefaultPeriodRunnerConfig(logger zerolog.Logger) PeriodRunnerConfig {
	return PeriodRunnerConfig{
		Handler:     nil,
		Epochs:      DefaultEpochsPerPeriod,
		GenesisTime: DefaultGenesisTime,
		Now:         time.Now,
		Logger:      logger.With().Str("component", "period-runner").Logger(),
	}
}

func (p *PeriodRunnerConfig) IsEmpty() bool {
	return p.Handler == nil &&
		p.Epochs == 0 &&
		p.GenesisTime.IsZero() &&
		p.Now == nil &&
		p.Logger.GetLevel() == zerolog.NoLevel
}

// LocalPeriodRunner implements PeriodRunner, emitting events according to a genesis time in an Ethereum epoch cadence.
// Event is emitted whenever genesis + K * periodDuration, for K = 0,1,2,...
type LocalPeriodRunner struct {
	handler        PeriodCallback
	periodDuration time.Duration
	now            func() time.Time
	genesisTime    time.Time
	log            zerolog.Logger
	cancel         context.CancelFunc
	started        bool
}

// NewLocalPeriodRunner constructs a LocalPeriodRunner using local time.
// If config.Handler is nil, SetHandler must be called before Start.
func NewLocalPeriodRunner(cfg PeriodRunnerConfig) PeriodRunner {

	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	if cfg.Epochs == 0 {
		cfg.Epochs = DefaultEpochsPerPeriod
	}
	period := time.Duration(cfg.Epochs) * time.Duration(EthSlotsPerEpoch) * EthSlotDuration

	return &LocalPeriodRunner{
		handler:        cfg.Handler,
		periodDuration: period,
		now:            cfg.Now,
		genesisTime:    cfg.GenesisTime,
		log:            cfg.Logger,
	}
}

func (r *LocalPeriodRunner) SetHandler(handler PeriodCallback) {
	r.handler = handler
}

// Start begins emitting periodDuration events until the context is cancelled or Stop is called.
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

func (r *LocalPeriodRunner) PeriodForTime(t time.Time) (uint64, time.Time) {
	if t.Before(r.genesisTime) {
		return 0, r.genesisTime
	}

	elapsed := t.Sub(r.genesisTime)
	currentPeriod := uint64(elapsed / r.periodDuration)
	start := r.periodStart(currentPeriod)
	return currentPeriod, start
}

func (r *LocalPeriodRunner) periodStart(periodID uint64) time.Time {
	return r.genesisTime.Add(time.Duration(periodID) * r.periodDuration)
}
