package mailbox

import (
	"bytes"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Mailbox contract ABI matching the deployed mailbox contract.
const mailboxABI = `[
  {
    "type":"function",
    "name":"putInbox",
    "inputs":[
      {
        "name":"chainMessageSender",
        "type":"uint256",
        "internalType":"uint256"
      },
      {
        "name":"sender",
        "type":"address",
        "internalType":"address"
      },
      {
        "name":"receiver",
        "type":"address",
        "internalType":"address"
      },
      {
        "name":"sessionId",
        "type":"uint256",
        "internalType":"uint256"
      },
      {
        "name":"label",
        "type":"bytes",
        "internalType":"bytes"
      },
      {
        "name":"data",
        "type":"bytes",
        "internalType":"bytes"
      }
    ],
    "outputs":[ ],
    "stateMutability":"nonpayable"
  },
  {
    "type":"function",
    "name":"read",
    "inputs":[
      {
        "name":"chainMessageSender",
        "type":"uint256",
        "internalType":"uint256"
      },
      {
        "name":"sender",
        "type":"address",
        "internalType":"address"
      },
      {
        "name":"sessionId",
        "type":"uint256",
        "internalType":"uint256"
      },
      {
        "name":"label",
        "type":"bytes",
        "internalType":"bytes"
      }
    ],
    "outputs":[
      {
        "name":"message",
        "type":"bytes",
        "internalType":"bytes"
      }
    ],
    "stateMutability":"view"
  },
  {
    "type":"function",
    "name":"write",
    "inputs":[
      {
        "name":"chainMessageRecipient",
        "type":"uint256",
        "internalType":"uint256"
      },
      {
        "name":"receiver",
        "type":"address",
        "internalType":"address"
      },
      {
        "name":"sessionId",
        "type":"uint256",
        "internalType":"uint256"
      },
      {
        "name":"label",
        "type":"bytes",
        "internalType":"bytes"
      },
      {
        "name":"data",
        "type":"bytes",
        "internalType":"bytes"
      }
    ],
    "outputs":[ ],
    "stateMutability":"nonpayable"
  }
]`

// Parser parses mailbox contract calls from EVM trace operations.
type Parser struct {
	chainID          uint64
	mailboxAddresses map[common.Address]bool
	parsedABI        abi.ABI
}

// NewParser creates a parser for the given chain's mailbox addresses.
func NewParser(chainID uint64, mailboxAddrs []common.Address) (*Parser, error) {
	parsed, err := abi.JSON(strings.NewReader(mailboxABI))
	if err != nil {
		return nil, fmt.Errorf("parse mailbox ABI: %w", err)
	}

	addrs := make(map[common.Address]bool)
	for _, addr := range mailboxAddrs {
		addrs[addr] = true
	}

	return &Parser{
		chainID:          chainID,
		mailboxAddresses: addrs,
		parsedABI:        parsed,
	}, nil
}

// ChainID returns the parser's chain ID.
func (p *Parser) ChainID() uint64 {
	return p.chainID
}

// IsMailboxAddress checks if the address is a tracked mailbox contract.
func (p *Parser) IsMailboxAddress(addr common.Address) bool {
	return p.mailboxAddresses[addr]
}

// AnalyzeTrace analyzes a callTracer result and extracts mailbox operations.
func (p *Parser) AnalyzeTrace(
	trace *CallTraceResult,
	alreadySentMsgs []CrossRollupMessage,
	fulfilledDeps []CrossRollupDependency,
) (*SimulationState, error) {
	state := &SimulationState{
		Success:          trace.Error == "",
		Dependencies:     make([]CrossRollupDependency, 0),
		OutboundMessages: make([]CrossRollupMessage, 0),
		GasUsed:          uint64(trace.GasUsed),
	}

	p.walkCallFrame(&trace.CallFrame, state, alreadySentMsgs, fulfilledDeps)

	return state, nil
}

// walkCallFrame recursively walks the call tree looking for mailbox operations.
func (p *Parser) walkCallFrame(
	frame *CallFrame,
	state *SimulationState,
	alreadySentMsgs []CrossRollupMessage,
	fulfilledDeps []CrossRollupDependency,
) {
	// Check if this call is to a mailbox address
	if p.IsMailboxAddress(frame.To) {
		// Handle CALL (write) and STATICCALL (read) operations
		if (frame.Type == "CALL" || frame.Type == "STATICCALL") && len(frame.Input) >= 4 {
			call, err := p.parseMailboxCall(frame.Input)
			if err == nil {
				// Process mailbox.read()
				if call.IsRead && p.awaitRead(call) {
					dep := CrossRollupDependency{
						SourceChainID: call.ChainSrc.Uint64(),
						DestChainID:   call.ChainDest.Uint64(),
						Sender:        call.Sender,
						Receiver:      frame.From,
						SessionID:     call.SessionID,
						Label:         call.Label,
						RequiredData:  true,
						IsInboxRead:   true,
					}

					if !ContainsDependency(fulfilledDeps, dep) {
						state.Dependencies = append(state.Dependencies, dep)
					}
				}

				// Process mailbox.write()
				if call.IsWrite && p.mustWrite(call) {
					msg := CrossRollupMessage{
						SourceChainID: call.ChainSrc.Uint64(),
						DestChainID:   call.ChainDest.Uint64(),
						Sender:        frame.From,
						Receiver:      call.Receiver,
						SessionID:     call.SessionID,
						Data:          call.Data,
						Label:         call.Label,
						MessageType:   "mailbox_write",
						IsOutboxWrite: true,
					}

					if !AlreadySent(alreadySentMsgs, msg) {
						state.OutboundMessages = append(state.OutboundMessages, msg)
					}
				}
			}
		}
	}

	// Recurse into nested calls
	for i := range frame.Calls {
		p.walkCallFrame(&frame.Calls[i], state, alreadySentMsgs, fulfilledDeps)
	}
}

// parseMailboxCall parses raw call data into a MailboxCall structure.
func (p *Parser) parseMailboxCall(callData []byte) (*MailboxCall, error) {
	if len(callData) < 4 {
		return nil, fmt.Errorf("call data too short")
	}

	methodSig := callData[:4]

	// Check for read() method
	if bytes.Equal(methodSig, p.parsedABI.Methods["read"].ID) {
		return p.parseReadCall(callData[4:])
	}

	// Check for write() method
	if bytes.Equal(methodSig, p.parsedABI.Methods["write"].ID) {
		return p.parseWriteCall(callData[4:])
	}

	return nil, fmt.Errorf("unknown mailbox method")
}

// parseReadCall parses a read() call.
// read(chainMessageSender, sender, sessionId, label)
func (p *Parser) parseReadCall(data []byte) (*MailboxCall, error) {
	values, err := p.parsedABI.Methods["read"].Inputs.Unpack(data)
	if err != nil {
		return nil, fmt.Errorf("unpack read call: %w", err)
	}

	call := &MailboxCall{
		ChainMessageSender: values[0].(*big.Int),
		Sender:             values[1].(common.Address),
		SessionID:          values[2].(*big.Int),
		Label:              values[3].([]byte),
		IsRead:             true,
	}
	call.ChainSrc = call.ChainMessageSender
	call.ChainDest = new(big.Int).SetUint64(p.chainID)
	return call, nil
}

// parseWriteCall parses a write() call.
// write(chainMessageRecipient, receiver, sessionId, label, data)
func (p *Parser) parseWriteCall(data []byte) (*MailboxCall, error) {
	values, err := p.parsedABI.Methods["write"].Inputs.Unpack(data)
	if err != nil {
		return nil, fmt.Errorf("unpack write call: %w", err)
	}

	call := &MailboxCall{
		ChainMessageRecipient: values[0].(*big.Int),
		Receiver:              values[1].(common.Address),
		SessionID:             values[2].(*big.Int),
		Label:                 values[3].([]byte),
		Data:                  values[4].([]byte),
		IsWrite:               true,
	}
	call.ChainSrc = new(big.Int).SetUint64(p.chainID)
	call.ChainDest = call.ChainMessageRecipient
	return call, nil
}

// mustWrite returns true if this chain should perform the write (source chain).
func (p *Parser) mustWrite(call *MailboxCall) bool {
	return call.ChainSrc.Uint64() == p.chainID && call.ChainDest.Uint64() != p.chainID
}

// awaitRead returns true if this chain should wait for data (destination chain).
func (p *Parser) awaitRead(call *MailboxCall) bool {
	return call.ChainSrc.Uint64() != p.chainID && call.ChainDest.Uint64() == p.chainID
}

// BuildPutInboxCallData builds the call data for a putInbox transaction.
func (p *Parser) BuildPutInboxCallData(dep CrossRollupDependency) ([]byte, error) {
	// putInbox(chainMessageSender, sender, receiver, sessionId, label, data)
	return p.parsedABI.Pack("putInbox",
		new(big.Int).SetUint64(dep.SourceChainID),
		dep.Sender,
		dep.Receiver,
		dep.SessionID,
		dep.Label,
		dep.Data,
	)
}

// ContainsDependency checks if a dependency is already in the list.
func ContainsDependency(deps []CrossRollupDependency, dep CrossRollupDependency) bool {
	for _, d := range deps {
		if d.SourceChainID == dep.SourceChainID &&
			d.DestChainID == dep.DestChainID &&
			d.Sender == dep.Sender &&
			d.Receiver == dep.Receiver &&
			sameBigInt(d.SessionID, dep.SessionID) &&
			bytes.Equal(d.Label, dep.Label) &&
			d.RequiredData == dep.RequiredData &&
			d.IsInboxRead == dep.IsInboxRead {
			return true
		}
	}
	return false
}

// AlreadySent checks if a message has already been sent.
func AlreadySent(msgs []CrossRollupMessage, msg CrossRollupMessage) bool {
	for _, m := range msgs {
		if m.SourceChainID == msg.SourceChainID &&
			m.DestChainID == msg.DestChainID &&
			m.Sender == msg.Sender &&
			m.Receiver == msg.Receiver &&
			sameBigInt(m.SessionID, msg.SessionID) &&
			bytes.Equal(m.Data, msg.Data) &&
			bytes.Equal(m.Label, msg.Label) &&
			m.MessageType == msg.MessageType &&
			m.IsOutboxWrite == msg.IsOutboxWrite {
			return true
		}
	}
	return false
}

func sameBigInt(a, b *big.Int) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return a.Cmp(b) == 0
	}
}
