package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/compose-network/specs/compose/proto"
)

func TestBasicValidator_ValidateStartPeriod(t *testing.T) {
	t.Parallel()

	validator := NewBasicValidator()

	tests := []struct {
		name        string
		startPeriod *pb.StartPeriod
		wantErr     bool
		errMsg      string
	}{
		{
			name: "valid StartPeriod passes validation",
			startPeriod: &pb.StartPeriod{
				PeriodId:         1,
				SuperblockNumber: 1,
			},
			wantErr: false,
		},
		{
			name:        "nil StartPeriod returns error",
			startPeriod: nil,
			wantErr:     true,
			errMsg:      "StartSC message is nil",
		},
		{
			name: "zero period ID returns error",
			startPeriod: &pb.StartPeriod{
				PeriodId:         0,
				SuperblockNumber: 1,
			},
			wantErr: true,
			errMsg:  "invalid period ID: 0",
		},
		{
			name: "zero superblock number returns error",
			startPeriod: &pb.StartPeriod{
				PeriodId:         1,
				SuperblockNumber: 0,
			},
			wantErr: true,
			errMsg:  "invalid superblock number: 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validator.ValidateStartPeriod(tt.startPeriod)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBasicValidator_ValidateStartInstance(t *testing.T) {
	t.Parallel()

	validator := NewBasicValidator()

	tests := []struct {
		name          string
		startInstance *pb.StartInstance
		wantErr       bool
		errMsg        string
	}{
		{
			name: "valid StartInstance passes validation",
			startInstance: &pb.StartInstance{
				InstanceId:     []byte("instance_id"),
				PeriodId:       1,
				SequenceNumber: 1,
				XtRequest: &pb.XTRequest{
					TransactionRequests: []*pb.TransactionRequest{
						{
							ChainId:     1,
							Transaction: [][]byte{[]byte("tx1")},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:          "nil StartInstance returns error",
			startInstance: nil,
			wantErr:       true,
			errMsg:        "StartSC message is nil",
		},
		{
			name: "missing instance ID returns error",
			startInstance: &pb.StartInstance{
				InstanceId:     []byte(""),
				PeriodId:       1,
				SequenceNumber: 1,
				XtRequest:      &pb.XTRequest{},
			},
			wantErr: true,
			errMsg:  "missing cross-chain transaction ID",
		},
		{
			name: "missing XT request returns error",
			startInstance: &pb.StartInstance{
				InstanceId:     []byte("instance_id"),
				PeriodId:       1,
				SequenceNumber: 1,
				XtRequest:      nil,
			},
			wantErr: true,
			errMsg:  "missing cross-chain transaction request",
		},
		{
			name: "invalid XT request returns error",
			startInstance: &pb.StartInstance{
				InstanceId:     []byte("instance_id"),
				PeriodId:       1,
				SequenceNumber: 1,
				XtRequest: &pb.XTRequest{
					TransactionRequests: []*pb.TransactionRequest{},
				},
			},
			wantErr: true,
			errMsg:  "invalid XTRequest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validator.ValidateStartInstance(tt.startInstance)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBasicValidator_ValidateRollback(t *testing.T) {
	t.Parallel()

	validator := NewBasicValidator()

	tests := []struct {
		name    string
		rb      *pb.Rollback
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid Rollback passes validation",
			rb: &pb.Rollback{
				PeriodId:                      1,
				LastFinalizedSuperblockNumber: 10,
				LastFinalizedSuperblockHash:   []byte("hash"),
			},
			wantErr: false,
		},
		{
			name:    "nil Rollback returns error",
			rb:      nil,
			wantErr: true,
			errMsg:  "rollback message is nil",
		},
		{
			name: "zero periodID returns error",
			rb: &pb.Rollback{
				PeriodId: 0,
			},
			wantErr: true,
			errMsg:  "invalid current period: 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validator.ValidateRollback(tt.rb)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func Test_ValidateXTRequest(t *testing.T) {
	t.Parallel()

	validator := &basicValidator{}

	tests := []struct {
		name    string
		xtReq   *pb.XTRequest
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid XTRequest passes validation",
			xtReq: &pb.XTRequest{
				TransactionRequests: []*pb.TransactionRequest{
					{
						ChainId:     1,
						Transaction: [][]byte{[]byte("tx1")},
					},
					{
						ChainId:     2,
						Transaction: [][]byte{[]byte("tx2")},
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "nil XTRequest returns error",
			xtReq:   nil,
			wantErr: true,
			errMsg:  "XTRequest is nil",
		},
		{
			name: "empty transactions returns error",
			xtReq: &pb.XTRequest{
				TransactionRequests: []*pb.TransactionRequest{},
			},
			wantErr: true,
			errMsg:  "no transactions in XTRequest",
		},
		{
			name: "invalid transaction request returns error",
			xtReq: &pb.XTRequest{
				TransactionRequests: []*pb.TransactionRequest{
					{
						ChainId:     0,
						Transaction: [][]byte{[]byte("tx1")},
					},
				},
			},
			wantErr: true,
			errMsg:  "invalid transaction request at index 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validator.validateXTRequest(tt.xtReq)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func Test_ValidateTransactionRequest(t *testing.T) {
	t.Parallel()

	validator := &basicValidator{}

	tests := []struct {
		name    string
		txReq   *pb.TransactionRequest
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid TransactionRequest passes validation",
			txReq: &pb.TransactionRequest{
				ChainId:     1,
				Transaction: [][]byte{[]byte("tx1"), []byte("tx2")},
			},
			wantErr: false,
		},
		{
			name:    "nil TransactionRequest returns error",
			txReq:   nil,
			wantErr: true,
			errMsg:  "TransactionRequest is nil",
		},
		{
			name: "missing chain ID returns error",
			txReq: &pb.TransactionRequest{
				ChainId:     0,
				Transaction: [][]byte{[]byte("tx1")},
			},
			wantErr: true,
			errMsg:  "missing chain ID",
		},
		{
			name: "no transactions returns error",
			txReq: &pb.TransactionRequest{
				ChainId:     1,
				Transaction: [][]byte{},
			},
			wantErr: true,
			errMsg:  "no transactions provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validator.validateTransactionRequest(tt.txReq)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
