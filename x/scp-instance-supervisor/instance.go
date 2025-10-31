package scpsupervisor

import (
	"sync"
	"time"

	"github.com/compose-network/specs/compose"
	"github.com/compose-network/specs/compose/scp"
)

// ActiveInstance tracks a running SCP instance.
type ActiveInstance struct {
	Key        string
	Instance   compose.Instance
	Runner     scp.PublisherInstance
	EnqueuedAt time.Time
	StartedAt  time.Time
	Timer      Timer
	finalOnce  sync.Once
}

// CompletedInstance represents a finalized SCP instance.
type CompletedInstance struct {
	Instance   compose.Instance
	Accepted   bool
	Source     DecisionSource
	EnqueuedAt time.Time
	StartedAt  time.Time
	DecidedAt  time.Time
}

// DecisionSource indicates why an instance finalized.
type DecisionSource string

const (
	DecisionSourceMessage DecisionSource = "message"
	DecisionSourceTimeout DecisionSource = "timeout"
)
