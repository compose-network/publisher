package scpsupervisor

import "time"

// Timer is used for trigerring SCP deadlines.
type Timer interface {
	Stop() bool
}

// TimerFactory creates a Timer that executes a function after a duration.
type TimerFactory interface {
	AfterFunc(duration time.Duration, fn func()) Timer
}

// SystemTimerFactory implements TimerFactory using the standard library time package.
type SystemTimerFactory struct{}

// AfterFunc creates a timer that runs fn after duration.
func (SystemTimerFactory) AfterFunc(duration time.Duration, fn func()) Timer {
	return &systemTimer{timer: time.AfterFunc(duration, fn)}
}

// systemTimer implements Timer using time.Timer.
type systemTimer struct {
	timer *time.Timer
}

func (t *systemTimer) Stop() bool {
	return t.timer.Stop()
}
