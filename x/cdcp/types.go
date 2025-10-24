package cdcp

type ChainID uint64
type Slot uint64
type SequenceNumber uint64
type XTId [32]byte
type XTRequest []TransactionRequest

type TransactionRequest struct {
	ChainID      ChainID
	Transactions [][]byte // RLP encoded Ethereum transactions
}

type InstanceData struct {
	Slot           Slot
	SequenceNumber SequenceNumber
	xTRequest      XTRequest
	xTId           XTId
}

type DecisionResult int

const (
	DecisionResultUndecided DecisionResult = iota
	DecisionResultAccepted
	DecisionResultRejected
)
