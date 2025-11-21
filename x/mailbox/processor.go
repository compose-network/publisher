package mailbox

import (
	"context"
	"fmt"
	"math/big"
	"strconv"

	rollupv1 "github.com/compose-network/publisher/proto/rollup/v1"
	"github.com/compose-network/publisher/x/tracer"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"
)

type processor struct {
	chainID          uint64
	mailboxAddresses []common.Address
	selector         MailboxSelector
	log              zerolog.Logger
	sender           MessageSender
	inbox            MessageInbox
	txBuilder        PutInboxTxBuilder
	waitCfg          WaitConfig
	mailboxABI       abi.ABI
}

var _ Processor = (*processor)(nil)

func (p *processor) AnalyzeTransaction(
	traceResult *tracer.SSVTraceResult,
	sentOutboundMsgs []CrossRollupMessage,
	fulfilledDeps []CrossRollupDependency,
	tx *types.Transaction,
) (*SimulationState, error) {
	txHashHex := tx.Hash().Hex()
	simState, err := p.analyzeTransaction(traceResult, sentOutboundMsgs, fulfilledDeps, txHashHex)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze transaction: %w", err)
	}

	simState.Tx = tx

	if !simState.RequiresCoordination() {
		p.log.Info().
			Str("tx_hash", txHashHex).
			Msg("Transaction requires no cross-rollup coordination")
		return simState, nil
	}

	p.log.Info().
		Str("tx_hash", txHashHex).
		Int("dependencies", len(simState.Dependencies)).
		Int("outbound", len(simState.OutboundMessages)).
		Msg("Transaction requires cross-rollup coordination")

	return simState, nil
}

func (p *processor) HandleCrossRollupCoordination(
	ctx context.Context,
	simState *SimulationState,
	xtID *rollupv1.XtID,
) ([]CrossRollupMessage, []CrossRollupDependency, error) {
	sentMsgs := make([]CrossRollupMessage, 0, len(simState.OutboundMessages))
	for _, outMsg := range simState.OutboundMessages {
		if err := p.SendCIRCMessage(ctx, &outMsg, xtID); err != nil {
			return nil, nil, fmt.Errorf("failed to send CIRC message: %w", err)
		}
		sentMsgs = append(sentMsgs, outMsg)
	}

	circDeps := make([]CrossRollupDependency, 0, len(simState.Dependencies))

	for _, dep := range simState.Dependencies {
		circMsg, err := p.inbox.WaitForDependency(ctx, xtID, dep, p.waitCfg)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to wait for CIRC message: %w", err)
		}

		if len(circMsg.Source) > 0 {
			dep.Sender = common.BytesToAddress(circMsg.Source[0])
		}
		if len(circMsg.Receiver) > 0 {
			dep.Receiver = common.BytesToAddress(circMsg.Receiver[0])
		}
		if len(circMsg.Data) > 0 {
			dep.Data = circMsg.Data[0]
		}
		if len(circMsg.SessionId) > 0 {
			dep.SessionID = new(big.Int).SetBytes(circMsg.SessionId)
		}

		circDeps = append(circDeps, dep)
	}

	p.log.Info().
		Str("xt_id", xtID.Hex()).
		Int("sent", len(sentMsgs)).
		Int("received", len(circDeps)).
		Msg("Cross-rollup coordination completed")

	return sentMsgs, circDeps, nil
}

func (p *processor) SendCIRCMessage(ctx context.Context, msg *CrossRollupMessage, xtID *rollupv1.XtID) error {
	var sessionID []byte
	if msg.SessionID != nil {
		sessionID = common.LeftPadBytes(msg.SessionID.Bytes(), 32)
	}

	circMsg := &rollupv1.CIRCMessage{
		SourceChain:      new(big.Int).SetUint64(msg.SourceChainID).Bytes(),
		DestinationChain: new(big.Int).SetUint64(msg.DestChainID).Bytes(),
		Source:           [][]byte{msg.Sender.Bytes()},
		Receiver:         [][]byte{msg.Receiver.Bytes()},
		XtId:             xtID,
		Label:            string(msg.Label),
		Data:             [][]byte{msg.Data},
		SessionId:        sessionID,
	}

	spMsg := &rollupv1.Message{
		SenderId: strconv.FormatUint(p.chainID, 10),
		Payload: &rollupv1.Message_CircMessage{
			CircMessage: circMsg,
		},
	}

	if err := p.sender.Send(ctx, msg.DestChainID, spMsg); err != nil {
		p.log.Error().
			Err(err).
			Str("xt_id", xtID.Hex()).
			Uint64("dest_chain", msg.DestChainID).
			Msg("Failed to send CIRC message")
		return err
	}

	return nil
}

func (p *processor) CreatePutInboxTx(dep CrossRollupDependency, nonce uint64) (*types.Transaction, error) {
	return p.txBuilder.BuildPutInboxTx(dep, nonce)
}
