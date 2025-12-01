package mempool

// Record represents a transaction in the mempool lifecycle.
type Record struct {
	Hash            string
	XtID            string
	Kind            TxKind
	Status          Status
	CommittedSlot   uint64
	CommittedBlock  uint64
	CreatedSlot     uint64
	LastUpdatedSlot uint64
	Nonce           uint64
	From            []byte
	Sequence        uint64
}

// TxKind identifies the transaction type in a cross-chain transaction pair.
type TxKind uint8

const (
	// KindOriginal is the user-initiated transaction.
	KindOriginal TxKind = iota
	// KindPutInbox is the coordinator-signed mailbox transaction.
	KindPutInbox
	// KindCIRCDelivery is a cross-chain message delivery transaction.
	KindCIRCDelivery
)

const unknownString = "unknown"

func (k TxKind) String() string {
	switch k {
	case KindOriginal:
		return "original"
	case KindPutInbox:
		return "putInbox"
	case KindCIRCDelivery:
		return "circDelivery"
	default:
		return unknownString
	}
}

// Status represents the transaction lifecycle stage.
type Status uint8

const (
	// StatusStaged indicates the transaction is queued for inclusion.
	StatusStaged Status = iota
	// StatusCommitted indicates the transaction is in a block.
	StatusCommitted
)

func (s Status) String() string {
	switch s {
	case StatusStaged:
		return "staged"
	case StatusCommitted:
		return "committed"
	default:
		return unknownString
	}
}

// State represents the sequencer's operational state.
type State string

const (
	StateWaiting        State = "Waiting"
	StateBuildingFree   State = "BuildingFree"
	StateBuildingLocked State = "BuildingLocked"
	StateSubmission     State = "Submission"
)

// Bundle represents an ordered group of transactions for block inclusion.
type Bundle struct {
	XtID   string
	Hashes []string
}
