package mailbox

import (
	"crypto/ecdsa"
	"math/big"

	"github.com/compose-network/publisher/x/superblock/sequencer"
	"github.com/compose-network/publisher/x/transport"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Config describes the dependencies required to process mailbox interactions.
type Config struct {
	ChainID              uint64
	MailboxAddresses     []common.Address
	SequencerClients     map[string]transport.Client
	SequencerCoordinator sequencer.Coordinator
	CoordinatorKey       *ecdsa.PrivateKey
	CoordinatorAddr      common.Address
	MailboxSelector      func(chainID uint64) common.Address
}

type MailboxCall struct {
	ChainMessageSender    *big.Int
	Sender                common.Address
	ChainMessageRecipient *big.Int
	Receiver              common.Address
	SessionId             *big.Int
	Label                 []byte
	Data                  []byte
	IsRead                bool
	IsWrite               bool
	ChainSrc              *big.Int
	ChainDest             *big.Int
}

type CrossRollupDependency struct {
	SourceChainID uint64
	DestChainID   uint64
	Sender        common.Address
	Receiver      common.Address
	SessionID     *big.Int
	Label         []byte
	RequiredData  bool
	IsInboxRead   bool
	Data          []byte
}

type CrossRollupMessage struct {
	SourceChainID uint64
	DestChainID   uint64
	Sender        common.Address
	Receiver      common.Address
	SessionID     *big.Int
	Data          []byte
	Label         []byte
	MessageType   string
	IsOutboxWrite bool
}

type SimulationState struct {
	Success          bool
	Dependencies     []CrossRollupDependency
	OutboundMessages []CrossRollupMessage
	Tx               *types.Transaction
}

func (s SimulationState) RequiresCoordination() bool {
	return len(s.Dependencies) > 0 || len(s.OutboundMessages) > 0
}
