package protocol

import (
	"fmt"

	pb "github.com/compose-network/specs/compose/proto"
)

// basicValidator implements basic validation for SBCP messages
type basicValidator struct{}

// NewBasicValidator creates a new basic SBCP message validator
func NewBasicValidator() Validator {
	return &basicValidator{}
}

// ValidateStartInstance validates StartInstance messages
func (v *basicValidator) ValidateStartPeriod(startPeriod *pb.StartPeriod) error {
	if startPeriod == nil {
		return fmt.Errorf("StartSC message is nil")
	}

	if startPeriod.GetPeriodId() == 0 {
		return fmt.Errorf("invalid period ID: %d", startPeriod.PeriodId)
	}

	if startPeriod.GetSuperblockNumber() == 0 {
		return fmt.Errorf("invalid superblock number: %d", startPeriod.SuperblockNumber)
	}

	return nil
}

// ValidateStartInstance validates StartInstance messages
func (v *basicValidator) ValidateStartInstance(startInstance *pb.StartInstance) error {
	if startInstance == nil {
		return fmt.Errorf("StartSC message is nil")
	}

	if len(startInstance.InstanceId) == 0 {
		return fmt.Errorf("missing cross-chain transaction ID")
	}

	if startInstance.XtRequest == nil {
		return fmt.Errorf("missing cross-chain transaction request")
	}

	if err := v.validateXTRequest(startInstance.XtRequest); err != nil {
		return fmt.Errorf("invalid XTRequest: %w", err)
	}

	return nil
}

// ValidateRollBackAndStartSlot validates rollback messages
func (v *basicValidator) ValidateRollback(rb *pb.Rollback) error {
	if rb == nil {
		return fmt.Errorf("rollback message is nil")
	}
	if rb.GetPeriodId() == 0 {
		return fmt.Errorf("invalid current period: %d", rb.PeriodId)
	}

	return nil
}

// validateXTRequest validates cross-chain transaction requests
func (v *basicValidator) validateXTRequest(xtReq *pb.XTRequest) error {
	if xtReq == nil {
		return fmt.Errorf("XTRequest is nil")
	}

	if len(xtReq.TransactionRequests) == 0 {
		return fmt.Errorf("no transactions in XTRequest")
	}

	for i, txReq := range xtReq.TransactionRequests {
		if err := v.validateTransactionRequest(txReq); err != nil {
			return fmt.Errorf("invalid transaction request at index %d: %w", i, err)
		}
	}

	return nil
}

// validateTransactionRequest validates individual transaction requests
func (v *basicValidator) validateTransactionRequest(txReq *pb.TransactionRequest) error {
	if txReq == nil {
		return fmt.Errorf("TransactionRequest is nil")
	}

	if txReq.ChainId == 0 {
		return fmt.Errorf("missing chain ID")
	}

	if len(txReq.Transaction) == 0 {
		return fmt.Errorf("no transactions provided")
	}

	return nil
}
