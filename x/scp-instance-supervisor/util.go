package scpsupervisor

import "github.com/compose-network/specs/compose"

func instanceIDString(id []byte) string {
	if len(id) == 0 {
		return ""
	}
	instanceID := compose.InstanceID{}
	copy(instanceID[:], id)
	return instanceID.String()
}

func cloneInstance(instance compose.Instance) compose.Instance {
	cloned := compose.Instance{ID: instance.ID, PeriodID: instance.PeriodID, SequenceNumber: instance.SequenceNumber, XTRequest: cloneXTRequest(instance.XTRequest)}
	return cloned
}

func cloneXTRequest(xtRequest compose.XTRequest) compose.XTRequest {
	cloned := compose.XTRequest{Transactions: make([]compose.TransactionRequest, 0, len(xtRequest.Transactions))}
	for _, tr := range xtRequest.Transactions {
		c := compose.TransactionRequest{ChainID: tr.ChainID, Transactions: make([][]byte, 0, len(tr.Transactions))}
		for _, t := range tr.Transactions {
			c.Transactions = append(c.Transactions, append([]byte{}, t...))
		}
		cloned.Transactions = append(cloned.Transactions, c)
	}
	return cloned
}
