package mailbox

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/compose-network/publisher/x/tracer"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

func (p *processor) analyzeTransaction(
	traceResult *tracer.SSVTraceResult,
	sentOutboundMsgs []CrossRollupMessage,
	fulfilledDeps []CrossRollupDependency,
	txHashHex string,
) (*SimulationState, error) {
	if traceResult == nil {
		return nil, fmt.Errorf("trace result is nil")
	}
	if traceResult.ExecutionResult == nil {
		return nil, fmt.Errorf("trace execution result missing")
	}

	simState := &SimulationState{
		Success:          traceResult.ExecutionResult.Err == nil,
		Dependencies:     make([]CrossRollupDependency, 0),
		OutboundMessages: make([]CrossRollupMessage, 0),
	}

	p.log.Info().
		Str("tx_hash", txHashHex).
		Bool("success", simState.Success).
		Int("operations", len(traceResult.Operations)).
		Msg("Analyzing transaction trace")

	if traceResult.ExecutionResult.Err != nil {
		p.log.Warn().
			Err(traceResult.ExecutionResult.Err).
			Str("tx_hash", txHashHex).
			Hex("revert", traceResult.ExecutionResult.Revert()).
			Msg("Cross-chain transaction reverted during simulation; continuing analysis")
	}

	for i, op := range traceResult.Operations {
		p.handleMailboxOperation(op, sentOutboundMsgs, fulfilledDeps, simState, i)
	}

	p.logSimulationSummary(simState, txHashHex)

	return simState, nil
}

func (p *processor) handleMailboxOperation(
	op tracer.SSVOperation,
	sentOutboundMsgs []CrossRollupMessage,
	fulfilledDeps []CrossRollupDependency,
	simState *SimulationState,
	opIndex int,
) {
	if !p.isMailboxAddress(op.Address) {
		return
	}

	p.log.Debug().
		Int("index", opIndex).
		Str("type", op.Type.String()).
		Str("address", op.Address.Hex()).
		Str("from", op.From.Hex()).
		Int("calldata_len", len(op.CallData)).
		Msg("Found mailbox operation")

	if op.Type != vm.CALL && op.Type != vm.STATICCALL {
		p.log.Debug().
			Str("type", op.Type.String()).
			Str("address", op.Address.Hex()).
			Msg("Ignoring non-CALL/STATICCALL operation to mailbox")
		return
	}

	if len(op.CallData) < 4 {
		return
	}

	call, err := p.parseMailboxCall(op.CallData)
	if err != nil {
		p.log.Debug().Err(err).Msg("Failed to parse mailbox call")
		return
	}

	p.logParsedCall(call)

	if call.IsRead {
		p.processMailboxRead(call, op, fulfilledDeps, simState)
	}
	if call.IsWrite {
		p.processMailboxWrite(call, op, sentOutboundMsgs, simState)
	}
}

func (p *processor) logParsedCall(call *MailboxCall) {
	if call.IsRead {
		p.log.Debug().
			Stringer("chain_message_sender", call.ChainMessageSender).
			Str("sender", call.Sender.Hex()).
			Stringer("session_id", call.SessionId).
			Str("label", string(call.Label)).
			Msg("Parsed mailbox read call")
		return
	}

	if call.IsWrite {
		p.log.Debug().
			Stringer("chain_message_recipient", call.ChainMessageRecipient).
			Str("receiver", call.Receiver.Hex()).
			Stringer("session_id", call.SessionId).
			Str("label", string(call.Label)).
			Int("data_len", len(call.Data)).
			Msg("Parsed mailbox write call")
	}
}

func (p *processor) processMailboxRead(
	call *MailboxCall,
	op tracer.SSVOperation,
	fulfilledDeps []CrossRollupDependency,
	simState *SimulationState,
) {
	if !awaitRead(call, p.chainID) {
		p.log.Debug().
			Uint64("chain_src", call.ChainSrc.Uint64()).
			Uint64("chain_dest", call.ChainDest.Uint64()).
			Uint64("local_chain", p.chainID).
			Msg("Ignore mailbox read call: chainDest is another chain")
		return
	}

	dep := CrossRollupDependency{
		SourceChainID: call.ChainSrc.Uint64(),
		DestChainID:   call.ChainDest.Uint64(),
		Sender:        call.Sender,
		Receiver:      op.From,
		SessionID:     call.SessionId,
		Label:         call.Label,
		RequiredData:  true,
		IsInboxRead:   true,
	}

	if containsDependency(fulfilledDeps, dep) {
		p.log.Debug().
			Uint64("chain_src", call.ChainSrc.Uint64()).
			Uint64("chain_dest", call.ChainDest.Uint64()).
			Uint64("local_chain", p.chainID).
			Msg("Ignore mailbox read call: already fulfilled")
		return
	}

	simState.Dependencies = append(simState.Dependencies, dep)

	p.log.Info().
		Uint64("chain_src", dep.SourceChainID).
		Uint64("chain_dest", dep.DestChainID).
		Str("sender", dep.Sender.Hex()).
		Str("receiver", dep.Receiver.Hex()).
		Stringer("session_id", dep.SessionID).
		Msg("Detected new mailbox read call")
}

func (p *processor) processMailboxWrite(
	call *MailboxCall,
	op tracer.SSVOperation,
	sentOutboundMsgs []CrossRollupMessage,
	simState *SimulationState,
) {
	if !mustWrite(call, p.chainID) {
		p.log.Debug().
			Uint64("chain_src", call.ChainSrc.Uint64()).
			Uint64("chain_dest", call.ChainDest.Uint64()).
			Uint64("local_chain", p.chainID).
			Msg("Ignore mailbox write call: chainSrc is another chain")
		return
	}

	msg := CrossRollupMessage{
		SourceChainID: call.ChainSrc.Uint64(),
		DestChainID:   call.ChainDest.Uint64(),
		Sender:        op.From,
		Receiver:      call.Receiver,
		SessionID:     call.SessionId,
		Data:          call.Data,
		Label:         call.Label,
		MessageType:   "mailbox_write",
		IsOutboxWrite: true,
	}

	if alreadySent(sentOutboundMsgs, msg) {
		p.log.Debug().
			Uint64("chain_src", call.ChainSrc.Uint64()).
			Uint64("chain_dest", call.ChainDest.Uint64()).
			Uint64("local_chain", p.chainID).
			Msg("Ignore mailbox write call: already sent")
		return
	}

	simState.OutboundMessages = append(simState.OutboundMessages, msg)

	p.log.Info().
		Uint64("chain_src", msg.SourceChainID).
		Uint64("chain_dest", msg.DestChainID).
		Str("sender", msg.Sender.Hex()).
		Str("receiver", msg.Receiver.Hex()).
		Stringer("session_id", msg.SessionID).
		Int("data_len", len(msg.Data)).
		Msg("Detected new mailbox write call")
}

func (p *processor) logSimulationSummary(simState *SimulationState, txHashHex string) {
	p.log.Info().
		Str("tx_hash", txHashHex).
		Bool("requires_coordination", simState.RequiresCoordination()).
		Int("dependencies", len(simState.Dependencies)).
		Int("outbound_messages", len(simState.OutboundMessages)).
		Msg("Transaction analysis complete")

	if !simState.RequiresCoordination() {
		return
	}

	depCount := len(simState.Dependencies)
	outCount := len(simState.OutboundMessages)
	depPreview := make([]string, 0, 2)
	for i := 0; i < depCount && i < 2; i++ {
		d := simState.Dependencies[i]
		depPreview = append(depPreview, fmt.Sprintf("%d:%s->%s", d.SourceChainID, d.Sender.Hex(), d.Receiver.Hex()))
	}
	outPreview := make([]string, 0, 2)
	for i := 0; i < outCount && i < 2; i++ {
		o := simState.OutboundMessages[i]
		outPreview = append(
			outPreview,
			fmt.Sprintf("%d:%s->%s:%s", o.DestChainID, o.Sender.Hex(), o.Receiver.Hex(), string(o.Label)),
		)
	}
	p.log.Info().
		Str("tx_hash", txHashHex).
		Int("deps", depCount).
		Interface("deps_preview", depPreview).
		Int("outbound", outCount).
		Interface("out_preview", outPreview).
		Msg("Coordination classification")
}

func (p *processor) parseMailboxCall(callData []byte) (*MailboxCall, error) {
	if len(callData) < 4 {
		return nil, fmt.Errorf("invalid call data length")
	}

	methodSig := callData[:4]

	if bytes.Equal(methodSig, p.mailboxABI.Methods["read"].ID) {
		call, err := p.parseReadCall(callData[4:])
		if err != nil {
			return nil, err
		}
		call.IsRead = true
		call.ChainSrc = call.ChainMessageSender
		call.ChainDest = new(big.Int).SetUint64(p.chainID)
		return call, nil
	}

	if bytes.Equal(methodSig, p.mailboxABI.Methods["write"].ID) {
		call, err := p.parseWriteCall(callData[4:])
		if err != nil {
			return nil, err
		}
		call.IsWrite = true
		call.ChainSrc = new(big.Int).SetUint64(p.chainID)
		call.ChainDest = call.ChainMessageRecipient
		return call, nil
	}

	return nil, fmt.Errorf("unknown mailbox method")
}

func (p *processor) parseReadCall(data []byte) (*MailboxCall, error) {
	values, err := p.mailboxABI.Methods["read"].Inputs.Unpack(data)
	if err != nil {
		return nil, err
	}

	call := &MailboxCall{
		ChainMessageSender: values[0].(*big.Int),
		Sender:             values[1].(common.Address),
		SessionId:          values[2].(*big.Int),
		Label:              values[3].([]byte),
	}
	call.ChainSrc = call.ChainMessageSender
	call.ChainDest = new(big.Int).SetUint64(p.chainID)
	return call, nil
}

func (p *processor) parseWriteCall(data []byte) (*MailboxCall, error) {
	values, err := p.mailboxABI.Methods["write"].Inputs.Unpack(data)
	if err != nil {
		return nil, err
	}

	call := &MailboxCall{
		ChainMessageRecipient: values[0].(*big.Int),
		Receiver:              values[1].(common.Address),
		SessionId:             values[2].(*big.Int),
		Label:                 values[3].([]byte),
		Data:                  values[4].([]byte),
	}
	call.ChainSrc = new(big.Int).SetUint64(p.chainID)
	call.ChainDest = call.ChainMessageRecipient
	return call, nil
}

func (p *processor) isMailboxAddress(addr common.Address) bool {
	for _, mailboxAddr := range p.mailboxAddresses {
		if addr == mailboxAddr {
			return true
		}
	}
	return false
}
