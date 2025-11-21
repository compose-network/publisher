package mailbox

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"time"

	appLog "github.com/compose-network/publisher/log"
	rollupv1 "github.com/compose-network/publisher/proto/rollup/v1"
	"github.com/compose-network/publisher/x/superblock/sequencer"
	"github.com/compose-network/publisher/x/tracer"
	"github.com/compose-network/publisher/x/transport"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"
)

// Processor exposes the mailbox processing surface used by the sequencer runtime.
type Processor interface {
	AnalyzeTransaction(
		traceResult *tracer.SSVTraceResult,
		sentOutboundMsgs []CrossRollupMessage,
		fulfilledDeps []CrossRollupDependency,
		tx *types.Transaction,
	) (*SimulationState, error)
	HandleCrossRollupCoordination(
		ctx context.Context,
		simState *SimulationState,
		xtID *rollupv1.XtID,
	) ([]CrossRollupMessage, []CrossRollupDependency, error)
	SendCIRCMessage(ctx context.Context, msg *CrossRollupMessage, xtID *rollupv1.XtID) error
	CreatePutInboxTx(dep CrossRollupDependency, nonce uint64) (*types.Transaction, error)
}

// MessageSender abstracts the transport used to deliver CIRC messages to peer sequencers.
type MessageSender interface {
	Send(ctx context.Context, destChainID uint64, msg *rollupv1.Message) error
}

// MessageInbox abstracts how we consume inbound CIRC messages that fulfill dependencies.
type MessageInbox interface {
	WaitForDependency(
		ctx context.Context,
		xtID *rollupv1.XtID,
		dep CrossRollupDependency,
		wait WaitConfig,
	) (*rollupv1.CIRCMessage, error)
}

// PutInboxTxBuilder is responsible for constructing putInbox transactions.
type PutInboxTxBuilder interface {
	BuildPutInboxTx(dep CrossRollupDependency, nonce uint64) (*types.Transaction, error)
}

// MailboxSelector resolves a mailbox address for a given chain.
type MailboxSelector interface {
	MailboxAddress(chainID uint64) (common.Address, bool)
}

// SelectorFunc adapts a function to the MailboxSelector interface.
type SelectorFunc func(chainID uint64) common.Address

func (f SelectorFunc) MailboxAddress(chainID uint64) (common.Address, bool) {
	if f == nil {
		return common.Address{}, false
	}
	addr := f(chainID)
	if addr == (common.Address{}) {
		return common.Address{}, false
	}
	return addr, true
}

// WaitConfig controls polling behavior when waiting for inbound CIRC messages.
type WaitConfig struct {
	Timeout      time.Duration
	PollInterval time.Duration
}

// Config describes the dependencies required to process mailbox interactions.
type Config struct {
	ChainID          uint64
	MailboxAddresses []common.Address
	MailboxSelector  MailboxSelector
	Logger           zerolog.Logger

	Sender    MessageSender
	Inbox     MessageInbox
	TxBuilder PutInboxTxBuilder
	Wait      WaitConfig

	// Legacy wiring kept for ease of migration.
	SequencerClients     map[string]transport.Client
	SequencerCoordinator sequencer.Coordinator
	CoordinatorKey       *ecdsa.PrivateKey
	CoordinatorAddr      common.Address
}

func (c Config) waitConfigWithDefaults() WaitConfig {
	waitCfg := c.Wait
	if waitCfg.Timeout == 0 {
		waitCfg.Timeout = 12 * time.Second
	}
	if waitCfg.PollInterval == 0 {
		waitCfg.PollInterval = 50 * time.Millisecond
	}
	return waitCfg
}

// NewProcessor builds a testable processor with explicit interfaces for all side effects.
func NewProcessor(cfg Config) (Processor, error) {
	if cfg.ChainID == 0 {
		return nil, fmt.Errorf("chain ID must be set")
	}

	logger := cfg.Logger
	if logger.GetLevel() == zerolog.NoLevel {
		logger = appLog.New("info", true).Logger
	}
	logger = logger.With().Str("component", "mailbox").Logger()

	waitCfg := cfg.waitConfigWithDefaults()

	selector := cfg.MailboxSelector
	if selector == nil {
		selector = SelectorFunc(func(uint64) common.Address { return common.Address{} })
	}

	sender := cfg.Sender
	if sender == nil && len(cfg.SequencerClients) > 0 {
		sender = newTransportSender(cfg.SequencerClients)
	}
	if sender == nil {
		return nil, fmt.Errorf("message sender not provided")
	}

	inbox := cfg.Inbox
	if inbox == nil && cfg.SequencerCoordinator != nil {
		inbox = newConsensusInbox(cfg.SequencerCoordinator.Consensus(), logger)
	}
	if inbox == nil {
		return nil, fmt.Errorf("message inbox not provided")
	}

	abiSpec, err := parseMailboxABI()
	if err != nil {
		return nil, fmt.Errorf("parse mailbox ABI: %w", err)
	}

	txBuilder := cfg.TxBuilder
	if txBuilder == nil {
		if cfg.CoordinatorKey == nil {
			return nil, fmt.Errorf("tx builder not provided and coordinator key missing")
		}
		txBuilder = newPutInboxBuilder(cfg.ChainID, selector, cfg.CoordinatorKey, abiSpec, logger)
	}

	addresses := make([]common.Address, len(cfg.MailboxAddresses))
	copy(addresses, cfg.MailboxAddresses)

	return &processor{
		chainID:          cfg.ChainID,
		mailboxAddresses: addresses,
		selector:         selector,
		log:              logger,
		sender:           sender,
		inbox:            inbox,
		txBuilder:        txBuilder,
		waitCfg:          waitCfg,
		mailboxABI:       abiSpec,
	}, nil
}
