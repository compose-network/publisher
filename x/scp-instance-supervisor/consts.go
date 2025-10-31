package scpsupervisor

import "time"

const (
	DefaultInstanceTimeout  = 3 * time.Second
	DefaultMaxHistory       = 1000
	DefaultHistoryRetention = 24 * time.Hour
)
