package consensus

import (
	"time"

	"github.com/compose-network/specs/compose"
)

const (
	StatePending = compose.DecisionStatePending
	StateCommit  = compose.DecisionStateAccepted
	StateAbort   = compose.DecisionStateRejected
)

// Config holds configuration for the consensus coordinator.
type Config struct {
	// NodeID identifies this node in the consensus protocol.
	NodeID string

	// IsLeader indicates whether this node is the consensus leader.
	// The leader makes final commit/abort decisions.
	IsLeader bool

	// Timeout is the maximum time to wait for all votes before aborting.
	Timeout time.Duration

	// MaxPending is the maximum number of pending transactions.
	MaxPending int

	// CleanupPeriod is how often to clean up old completed transactions.
	CleanupPeriod time.Duration
}

// DefaultConfig returns a Config with default values.
func DefaultConfig() Config {
	return Config{
		Timeout:       60 * time.Second,
		MaxPending:    1000,
		CleanupPeriod: 5 * time.Minute,
	}
}

// StartFn is called when a new transaction should be broadcast.
type StartFn func(xtID string, participantChains []compose.ChainID, data []byte) error

// VoteFn is called when this node should send its vote.
type VoteFn func(xtID string, chainID compose.ChainID, vote bool) error

// DecisionFn is called when a decision has been made (or received from leader).
type DecisionFn func(xtID string, decision bool) error

// TransactionState holds the state of a single 2PC transaction.
type TransactionState struct {
	// ID is the transaction identifier.
	ID string

	// ParticipantChains are the chain IDs participating in this transaction.
	ParticipantChains []compose.ChainID

	// Votes maps chain ID to vote (true=commit, false=abort).
	Votes map[compose.ChainID]bool

	// Decision is the current decision state.
	Decision compose.DecisionState

	// Data holds any additional transaction data.
	Data []byte

	// StartTime is when the transaction was created.
	StartTime time.Time

	// DecidedTime is when the decision was made (if decided).
	DecidedTime time.Time
}

// VoteCount returns the number of votes received.
func (t *TransactionState) VoteCount() int {
	return len(t.Votes)
}

// AllVotesReceived returns true if all participants have voted.
func (t *TransactionState) AllVotesReceived() bool {
	return len(t.Votes) == len(t.ParticipantChains)
}

// AllVotesCommit returns true if all received votes are commits.
func (t *TransactionState) AllVotesCommit() bool {
	for _, vote := range t.Votes {
		if !vote {
			return false
		}
	}
	return true
}

// HasAbortVote returns true if any vote is an abort.
func (t *TransactionState) HasAbortVote() bool {
	for _, vote := range t.Votes {
		if !vote {
			return true
		}
	}
	return false
}

// IsDecided returns true if a final decision has been made.
func (t *TransactionState) IsDecided() bool {
	return t.Decision != StatePending
}

// Duration returns how long the transaction has been active.
func (t *TransactionState) Duration() time.Duration {
	if t.IsDecided() {
		return t.DecidedTime.Sub(t.StartTime)
	}
	return time.Since(t.StartTime)
}
