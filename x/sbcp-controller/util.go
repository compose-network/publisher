package sbcpcontroller

import (
	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
)

func clonePBXTRequest(req *pb.XTRequest) *pb.XTRequest {
	if req == nil {
		return nil
	}
	clone := &pb.XTRequest{TransactionRequests: make([]*pb.TransactionRequest, 0, len(req.TransactionRequests))}
	for _, tr := range req.TransactionRequests {
		txCopy := make([][]byte, len(tr.Transaction))
		for i, raw := range tr.Transaction {
			txCopy[i] = append([]byte(nil), raw...)
		}
		clone.TransactionRequests = append(clone.TransactionRequests, &pb.TransactionRequest{ChainId: tr.ChainId, Transaction: txCopy})
	}
	return clone
}

func protoXTRequestToCompose(req *pb.XTRequest) *compose.XTRequest {
	if req == nil {
		return nil
	}
	composeReq := &compose.XTRequest{Transactions: make([]compose.TransactionRequest, 0, len(req.TransactionRequests))}
	for _, tr := range req.TransactionRequests {
		txns := make([][]byte, 0, len(tr.Transaction))
		for _, raw := range tr.Transaction {
			txns = append(txns, append([]byte(nil), raw...))
		}
		composeReq.Transactions = append(composeReq.Transactions, compose.TransactionRequest{ChainID: compose.ChainID(tr.ChainId), Transactions: txns})
	}
	return composeReq
}
