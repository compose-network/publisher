package consensus

import (
	"context"

	pb "github.com/compose-network/publisher/proto/rollup/v1"
	"github.com/compose-network/publisher/x/cdcp"
)

type CdcpMessenger struct {
	c   *coordinator
	ctx context.Context
}

func NewCdcpMessenger(ctx context.Context, c *coordinator) *CdcpMessenger {
	return &CdcpMessenger{
		c:   c,
		ctx: ctx,
	}
}

func (m *CdcpMessenger) SendStartMessage(_ cdcp.Slot, _ cdcp.SequenceNumber, _ cdcp.XTRequest, _ cdcp.XTId) {
	// Don't do anything because superblock/coordinator already sends StartSC messages
}

func (m *CdcpMessenger) SendNativeDecided(xtId cdcp.XTId, decision bool) {
	m.c.callbackMgr.InvokeNativeDecided(cdcpXTIDToPbXTID(xtId), decision)
}

func (m *CdcpMessenger) SendDecided(xtId cdcp.XTId, decision bool) {
	m.c.callbackMgr.InvokeDecidedToNative(cdcpXTIDToPbXTID(xtId), decision)
}

func convertToCdcpXTRequest(xtRequest *pb.XTRequest) cdcp.XTRequest {
	var cdcpXTReq cdcp.XTRequest
	for _, txReq := range xtRequest.Transactions {
		cdcpXTReq = append(cdcpXTReq, cdcp.TransactionRequest{
			ChainID:      cdcp.ChainID(txReq.ChainId),
			Transactions: txReq.Transaction,
		})
	}
	return cdcpXTReq
}

func cdcpInstanceKey(xtId []byte) string {
	return string(xtId[:])
}

func cdcpXTIDToPbXTID(xtId cdcp.XTId) *pb.XtID {
	return &pb.XtID{
		Hash: xtId[:],
	}
}

func cdcpToCdcpXTID(xtId *pb.XtID) cdcp.XTId {
	var id cdcp.XTId
	copy(id[:], xtId.Hash)
	return id
}

func cdcpDecisionStateToDecisionState(state cdcp.DecisionResult) DecisionState {
	switch state {
	case cdcp.DecisionResultUndecided:
		return StateUndecided
	case cdcp.DecisionResultAccepted:
		return StateCommit
	case cdcp.DecisionResultRejected:
		return StateAbort
	default:
		return StateUndecided
	}
}
