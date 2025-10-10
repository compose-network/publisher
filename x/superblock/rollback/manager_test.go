package rollback

import (
	"errors"
	"testing"

	"github.com/rs/zerolog"
	l1events "github.com/ssvlabs/rollup-shared-publisher/x/superblock/l1/events"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/store"
)

func TestRollbackValidation(t *testing.T) {
	logger := zerolog.Nop()
	deps := Dependencies{
		Logger: logger,
	}

	// Create a minimal mock execution manager
	execManager := &mockExecManager{}
	manager := NewManager(deps, execManager)

	tests := []struct {
		name        string
		event       *l1events.SuperblockEvent
		rolledBack  *store.Superblock
		expectError bool
	}{
		{
			name: "valid rollback",
			event: &l1events.SuperblockEvent{
				SuperblockNumber: 3,
			},
			rolledBack: &store.Superblock{
				Number: 3,
				Slot:   30,
			},
			expectError: false,
		},
		{
			name: "nil superblock",
			event: &l1events.SuperblockEvent{
				SuperblockNumber: 3,
			},
			rolledBack:  nil,
			expectError: true,
		},
		{
			name: "genesis rollback",
			event: &l1events.SuperblockEvent{
				SuperblockNumber: 0,
			},
			rolledBack: &store.Superblock{
				Number: 0,
			},
			expectError: true,
		},
		{
			name: "number mismatch",
			event: &l1events.SuperblockEvent{
				SuperblockNumber: 5,
			},
			rolledBack: &store.Superblock{
				Number: 3,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.validateRollbackEvent(tt.event, tt.rolledBack)
			if (err != nil) != tt.expectError {
				t.Errorf("validateRollbackEvent() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestErrorTypes(t *testing.T) {
	validationErr := NewValidationError("test validation error")
	if validationErr.Type != ErrorTypeValidation {
		t.Errorf("Expected validation error type, got %v", validationErr.Type)
	}

	recoveryErr := NewRecoveryError("test recovery error").
		WithCause(validationErr).
		WithSuperblock(123).
		WithSlot(456)

	if recoveryErr.Type != ErrorTypeRecovery {
		t.Errorf("Expected recovery error type, got %v", recoveryErr.Type)
	}

	if recoveryErr.SuperblockNumber != 123 {
		t.Errorf("Expected superblock number 123, got %v", recoveryErr.SuperblockNumber)
	}

	if recoveryErr.Slot != 456 {
		t.Errorf("Expected slot 456, got %v", recoveryErr.Slot)
	}

	if !errors.Is(validationErr, recoveryErr.Unwrap()) {
		t.Errorf("Expected wrapped error to be validation error")
	}
}

// mockExecManager implements minimal ExecutionManage
type mockExecManager struct{}

func (m *mockExecManager) GetExecutionHistory(slot uint64) (*SlotExecution, bool) {
	return nil, false
}

func (m *mockExecManager) GetCurrentExecution() *SlotExecution {
	return nil
}

func (m *mockExecManager) SetCurrentExecution(exec *SlotExecution) {}

func (m *mockExecManager) ClearCurrentExecution() {}

func (m *mockExecManager) SyncExecutionFromStateMachine() {}

func (m *mockExecManager) RecordExecutionSnapshot(exec *SlotExecution) {}

func (m *mockExecManager) CleanupExecutionHistory(beforeSlot uint64) {}
