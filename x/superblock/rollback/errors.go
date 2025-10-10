package rollback

import (
	"fmt"
)

// ErrorType represents different categories of rollback errors
type ErrorType int

const (
	ErrorTypeValidation ErrorType = iota
	ErrorTypeRecovery
	ErrorTypeTransactionRequeue
	ErrorTypeStateMachine
	ErrorTypeBroadcast
)

// String returns the string representation of ErrorType
func (e ErrorType) String() string {
	switch e {
	case ErrorTypeValidation:
		return "validation"
	case ErrorTypeRecovery:
		return "recovery"
	case ErrorTypeTransactionRequeue:
		return "transaction_requeue"
	case ErrorTypeStateMachine:
		return "state_machine"
	case ErrorTypeBroadcast:
		return "broadcast"
	default:
		return "unknown"
	}
}

// RollbackError represents a structured error for rollback operations
type RollbackError struct {
	Type             ErrorType
	Message          string
	Cause            error
	Context          map[string]interface{}
	SuperblockNumber uint64
	Slot             uint64
}

// Error implements the error interface
func (e *RollbackError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("rollback %s error: %s: %v", e.Type, e.Message, e.Cause)
	}
	return fmt.Sprintf("rollback %s error: %s", e.Type, e.Message)
}

// Unwrap returns the underlying cause error
func (e *RollbackError) Unwrap() error {
	return e.Cause
}

// NewRollbackError creates a new rollback error with the specified type and message
func NewRollbackError(errType ErrorType, message string) *RollbackError {
	return &RollbackError{
		Type:    errType,
		Message: message,
		Context: make(map[string]interface{}),
	}
}

// WithCause adds a cause error to the rollback error
func (e *RollbackError) WithCause(cause error) *RollbackError {
	e.Cause = cause
	return e
}

// WithContext adds context information to the rollback error
func (e *RollbackError) WithContext(key string, value interface{}) *RollbackError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// WithSuperblock adds superblock information to the rollback error
func (e *RollbackError) WithSuperblock(number uint64) *RollbackError {
	e.SuperblockNumber = number
	return e
}

// WithSlot adds slot information to the rollback error
func (e *RollbackError) WithSlot(slot uint64) *RollbackError {
	e.Slot = slot
	return e
}

// Validation error helpers
func NewValidationError(message string) *RollbackError {
	return NewRollbackError(ErrorTypeValidation, message)
}

func NewRecoveryError(message string) *RollbackError {
	return NewRollbackError(ErrorTypeRecovery, message)
}

func NewTransactionRequeueError(message string) *RollbackError {
	return NewRollbackError(ErrorTypeTransactionRequeue, message)
}

func NewStateMachineError(message string) *RollbackError {
	return NewRollbackError(ErrorTypeStateMachine, message)
}

func NewBroadcastError(message string) *RollbackError {
	return NewRollbackError(ErrorTypeBroadcast, message)
}
