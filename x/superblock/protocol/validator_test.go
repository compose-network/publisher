package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/compose-network/publisher/proto/rollup/v1"
)

func TestBasicValidator_ValidateStartSlot(t *testing.T) {
	t.Parallel()

	validator := NewBasicValidator()

	tests := []struct {
		name      string
		startSlot *pb.StartSlot
		wantErr   bool
		errMsg    string
	}{
		{
			name: "valid StartSlot passes validation",
			startSlot: &pb.StartSlot{
				Slot:                 1,
				NextSuperblockNumber: 1,
				LastSuperblockHash:   []byte("genesis_hash"),
				L2BlocksRequest: []*pb.L2BlockRequest{
					{
						ChainId:     []byte("chain1"),
						BlockNumber: 1,
						ParentHash:  []byte("parent_hash"),
					},
				},
			},
			wantErr: false,
		},
		{
			name:      "nil StartSlot returns error",
			startSlot: nil,
			wantErr:   true,
			errMsg:    "StartSlot message is nil",
		},
		{
			name: "zero slot number returns error",
			startSlot: &pb.StartSlot{
				Slot:                 0,
				NextSuperblockNumber: 1,
				L2BlocksRequest:      []*pb.L2BlockRequest{},
			},
			wantErr: true,
			errMsg:  "invalid slot number: 0",
		},
		{
			name: "zero superblock number returns error",
			startSlot: &pb.StartSlot{
				Slot:                 1,
				NextSuperblockNumber: 0,
				L2BlocksRequest:      []*pb.L2BlockRequest{},
			},
			wantErr: true,
			errMsg:  "invalid superblock number: 0",
		},
		{
			name: "empty L2 block requests returns error",
			startSlot: &pb.StartSlot{
				Slot:                 1,
				NextSuperblockNumber: 1,
				L2BlocksRequest:      []*pb.L2BlockRequest{},
			},
			wantErr: true,
			errMsg:  "no L2 block requests in StartSlot",
		},
		{
			name: "invalid L2 block request returns error",
			startSlot: &pb.StartSlot{
				Slot:                 1,
				NextSuperblockNumber: 1,
				L2BlocksRequest: []*pb.L2BlockRequest{
					{
						ChainId:     []byte(""),
						BlockNumber: 1,
					},
				},
			},
			wantErr: true,
			errMsg:  "invalid L2 block request at index 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validator.ValidateStartSlot(tt.startSlot)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBasicValidator_ValidateRequestSeal(t *testing.T) {
	t.Parallel()

	validator := NewBasicValidator()

	tests := []struct {
		name        string
		requestSeal *pb.RequestSeal
		wantErr     bool
		errMsg      string
	}{
		{
			name: "valid RequestSeal with included XTs passes validation",
			requestSeal: &pb.RequestSeal{
				Slot:        1,
				IncludedXts: [][]byte{[]byte("xt1"), []byte("xt2")},
			},
			wantErr: false,
		},
		{
			name: "valid RequestSeal with no XTs passes validation",
			requestSeal: &pb.RequestSeal{
				Slot:        1,
				IncludedXts: [][]byte{},
			},
			wantErr: false,
		},
		{
			name:        "nil RequestSeal returns error",
			requestSeal: nil,
			wantErr:     true,
			errMsg:      "RequestSeal message is nil",
		},
		{
			name: "zero slot number returns error",
			requestSeal: &pb.RequestSeal{
				Slot:        0,
				IncludedXts: [][]byte{},
			},
			wantErr: true,
			errMsg:  "invalid slot number: 0",
		},
		{
			name: "empty XT ID returns error",
			requestSeal: &pb.RequestSeal{
				Slot:        1,
				IncludedXts: [][]byte{[]byte("xt1"), []byte("")},
			},
			wantErr: true,
			errMsg:  "empty XT ID at index 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validator.ValidateRequestSeal(tt.requestSeal)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBasicValidator_ValidateL2Block(t *testing.T) {
	t.Parallel()

	validator := NewBasicValidator()

	tests := []struct {
		name    string
		l2Block *pb.L2Block
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid L2Block passes validation",
			l2Block: &pb.L2Block{
				Slot:            1,
				ChainId:         []byte("chain1"),
				BlockNumber:     1,
				BlockHash:       []byte("block_hash"),
				ParentBlockHash: []byte("parent_hash"),
				IncludedXts:     [][]byte{[]byte("xt1")},
				Block:           []byte("encoded_block_data"),
			},
			wantErr: false,
		},
		{
			name:    "nil L2Block returns error",
			l2Block: nil,
			wantErr: true,
			errMsg:  "L2Block message is nil",
		},
		{
			name: "zero slot number returns error",
			l2Block: &pb.L2Block{
				Slot:        0,
				ChainId:     []byte("chain1"),
				BlockNumber: 1,
				BlockHash:   []byte("hash"),
				Block:       []byte("data"),
			},
			wantErr: true,
			errMsg:  "invalid slot number: 0",
		},
		{
			name: "missing chain ID returns error",
			l2Block: &pb.L2Block{
				Slot:        1,
				ChainId:     []byte(""),
				BlockNumber: 1,
				BlockHash:   []byte("hash"),
				Block:       []byte("data"),
			},
			wantErr: true,
			errMsg:  "missing chain ID",
		},
		{
			name: "zero block number is valid (genesis block)",
			l2Block: &pb.L2Block{
				Slot:        1,
				ChainId:     []byte("chain1"),
				BlockNumber: 0,
				BlockHash:   []byte("hash"),
				Block:       []byte("data"),
			},
			wantErr: false,
		},
		{
			name: "missing block hash returns error",
			l2Block: &pb.L2Block{
				Slot:        1,
				ChainId:     []byte("chain1"),
				BlockNumber: 1,
				BlockHash:   []byte(""),
				Block:       []byte("data"),
			},
			wantErr: true,
			errMsg:  "missing block hash",
		},
		{
			name: "missing block data returns error",
			l2Block: &pb.L2Block{
				Slot:        1,
				ChainId:     []byte("chain1"),
				BlockNumber: 1,
				BlockHash:   []byte("hash"),
				Block:       []byte(""),
			},
			wantErr: true,
			errMsg:  "missing block data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validator.ValidateL2Block(tt.l2Block)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBasicValidator_ValidateStartSC(t *testing.T) {
	t.Parallel()

	validator := NewBasicValidator()

	tests := []struct {
		name    string
		startSC *pb.StartSC
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid StartSC passes validation",
			startSC: &pb.StartSC{
				Slot:             1,
				XtSequenceNumber: 1,
				XtId:             []byte("xt_id"),
				XtRequest: &pb.XTRequest{
					Transactions: []*pb.TransactionRequest{
						{
							ChainId:     []byte("chain1"),
							Transaction: [][]byte{[]byte("tx1")},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "nil StartSC returns error",
			startSC: nil,
			wantErr: true,
			errMsg:  "StartSC message is nil",
		},
		{
			name: "zero slot number returns error",
			startSC: &pb.StartSC{
				Slot:             0,
				XtSequenceNumber: 1,
				XtId:             []byte("xt_id"),
				XtRequest:        &pb.XTRequest{},
			},
			wantErr: true,
			errMsg:  "invalid slot number: 0",
		},
		{
			name: "missing XT ID returns error",
			startSC: &pb.StartSC{
				Slot:             1,
				XtSequenceNumber: 1,
				XtId:             []byte(""),
				XtRequest:        &pb.XTRequest{},
			},
			wantErr: true,
			errMsg:  "missing cross-chain transaction ID",
		},
		{
			name: "missing XT request returns error",
			startSC: &pb.StartSC{
				Slot:             1,
				XtSequenceNumber: 1,
				XtId:             []byte("xt_id"),
				XtRequest:        nil,
			},
			wantErr: true,
			errMsg:  "missing cross-chain transaction request",
		},
		{
			name: "invalid XT request returns error",
			startSC: &pb.StartSC{
				Slot:             1,
				XtSequenceNumber: 1,
				XtId:             []byte("xt_id"),
				XtRequest: &pb.XTRequest{
					Transactions: []*pb.TransactionRequest{},
				},
			},
			wantErr: true,
			errMsg:  "invalid XTRequest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validator.ValidateStartSC(tt.startSC)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBasicValidator_ValidateRollBackAndStartSlot(t *testing.T) {
	t.Parallel()

	validator := NewBasicValidator()

	tests := []struct {
		name    string
		rb      *pb.RollBackAndStartSlot
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid RollBackAndStartSlot passes validation",
			rb: &pb.RollBackAndStartSlot{
				CurrentSlot:          1,
				NextSuperblockNumber: 1,
				LastSuperblockHash:   []byte("hash"),
				L2BlocksRequest: []*pb.L2BlockRequest{
					{
						ChainId:     []byte("chain1"),
						BlockNumber: 1,
						ParentHash:  []byte("parent"),
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "nil RollBackAndStartSlot returns error",
			rb:      nil,
			wantErr: true,
			errMsg:  "RollBackAndStartSlot message is nil",
		},
		{
			name: "zero current slot returns error",
			rb: &pb.RollBackAndStartSlot{
				CurrentSlot:          0,
				NextSuperblockNumber: 1,
				L2BlocksRequest:      []*pb.L2BlockRequest{},
			},
			wantErr: true,
			errMsg:  "invalid current slot: 0",
		},
		{
			name: "zero superblock number returns error",
			rb: &pb.RollBackAndStartSlot{
				CurrentSlot:          1,
				NextSuperblockNumber: 0,
				L2BlocksRequest:      []*pb.L2BlockRequest{},
			},
			wantErr: true,
			errMsg:  "invalid superblock number: 0",
		},
		{
			name: "empty L2 block requests returns error",
			rb: &pb.RollBackAndStartSlot{
				CurrentSlot:          1,
				NextSuperblockNumber: 1,
				L2BlocksRequest:      []*pb.L2BlockRequest{},
			},
			wantErr: true,
			errMsg:  "no L2 block requests in RollBackAndStartSlot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validator.ValidateRollBackAndStartSlot(tt.rb)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func Test_validateL2BlockRequest(t *testing.T) {
	t.Parallel()

	validator := &basicValidator{}

	tests := []struct {
		name    string
		req     *pb.L2BlockRequest
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid L2BlockRequest passes validation",
			req: &pb.L2BlockRequest{
				ChainId:     []byte("chain1"),
				BlockNumber: 1,
				ParentHash:  []byte("parent_hash"),
			},
			wantErr: false,
		},
		{
			name: "valid L2BlockRequest with empty parent hash passes validation",
			req: &pb.L2BlockRequest{
				ChainId:     []byte("chain1"),
				BlockNumber: 1,
				ParentHash:  []byte(""),
			},
			wantErr: false,
		},
		{
			name:    "nil L2BlockRequest returns error",
			req:     nil,
			wantErr: true,
			errMsg:  "L2BlockRequest is nil",
		},
		{
			name: "missing chain ID returns error",
			req: &pb.L2BlockRequest{
				ChainId:     []byte(""),
				BlockNumber: 1,
			},
			wantErr: true,
			errMsg:  "missing chain ID",
		},
		{
			name: "zero block number is valid (genesis block)",
			req: &pb.L2BlockRequest{
				ChainId:     []byte("chain1"),
				BlockNumber: 0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validator.validateL2BlockRequest(tt.req)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func Test_validateXTRequest(t *testing.T) {
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
				Transactions: []*pb.TransactionRequest{
					{
						ChainId:     []byte("chain1"),
						Transaction: [][]byte{[]byte("tx1")},
					},
					{
						ChainId:     []byte("chain2"),
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
				Transactions: []*pb.TransactionRequest{},
			},
			wantErr: true,
			errMsg:  "no transactions in XTRequest",
		},
		{
			name: "invalid transaction request returns error",
			xtReq: &pb.XTRequest{
				Transactions: []*pb.TransactionRequest{
					{
						ChainId:     []byte(""),
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

func Test_validateTransactionRequest(t *testing.T) {
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
				ChainId:     []byte("chain1"),
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
				ChainId:     []byte(""),
				Transaction: [][]byte{[]byte("tx1")},
			},
			wantErr: true,
			errMsg:  "missing chain ID",
		},
		{
			name: "no transactions returns error",
			txReq: &pb.TransactionRequest{
				ChainId:     []byte("chain1"),
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
