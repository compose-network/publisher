package periodrunner

import (
	"context"
	"time"
)

// PeriodRunner invokes the handler whenever a new SBCP period starts.
type PeriodRunner interface {
	SetHandler(PeriodCallback)
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	// PeriodForTime returns the period ID and the period start time for the given timestamp.
	PeriodForTime(t time.Time) (periodID uint64, periodStartTime time.Time)
}

// PeriodCallback is the hook invoked by PeriodRunner for each new period.
type PeriodCallback func(context.Context, PeriodInfo) error

// PeriodInfo represents an SBCP period and is provided as the argument to the PeriodCallback hook.
type PeriodInfo struct {
	PeriodID  uint64
	StartedAt time.Time
	Duration  time.Duration
}
