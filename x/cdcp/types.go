package cdcp

type ChainID string

func (c *ChainID) Equal(other *ChainID) bool {
	// Returns true if both strings are equal
	return *c == *other
}

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
	XTRequest      XTRequest
	XTId           XTId
}

type DecisionResult int

const (
	DecisionResultUndecided DecisionResult = iota
	DecisionResultAccepted
	DecisionResultRejected
)

func (d DecisionResult) String() string {
	switch d {
	case DecisionResultUndecided:
		return "Undecided"
	case DecisionResultAccepted:
		return "Accepted"
	case DecisionResultRejected:
		return "Rejected"
	default:
		return "Unknown"
	}
}
