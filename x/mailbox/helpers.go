package mailbox

import (
	"bytes"

	rollupv1 "github.com/compose-network/publisher/proto/rollup/v1"
	"github.com/ethereum/go-ethereum/common"
)

func alreadySent(msgs []CrossRollupMessage, msg CrossRollupMessage) bool {
	for _, m := range msgs {
		if m.SourceChainID == msg.SourceChainID &&
			m.DestChainID == msg.DestChainID &&
			m.Sender == msg.Sender &&
			m.Receiver == msg.Receiver &&
			m.SessionID.Cmp(msg.SessionID) == 0 &&
			bytes.Equal(m.Data, msg.Data) &&
			bytes.Equal(m.Label, msg.Label) &&
			m.MessageType == msg.MessageType &&
			m.IsOutboxWrite == msg.IsOutboxWrite {
			return true
		}
	}

	return false
}

func containsDependency(deps []CrossRollupDependency, dep CrossRollupDependency) bool {
	for _, d := range deps {
		if d.SourceChainID == dep.SourceChainID &&
			d.DestChainID == dep.DestChainID &&
			d.Sender == dep.Sender &&
			d.Receiver == dep.Receiver &&
			d.SessionID.Cmp(dep.SessionID) == 0 &&
			bytes.Equal(d.Label, dep.Label) &&
			d.RequiredData == dep.RequiredData &&
			d.IsInboxRead == dep.IsInboxRead {
			return true
		}
	}

	return false
}

func mustWrite(call *MailboxCall, chainID uint64) bool {
	return call.ChainSrc.Uint64() == chainID && call.ChainDest.Uint64() != chainID
}

func awaitRead(call *MailboxCall, chainID uint64) bool {
	return call.ChainSrc.Uint64() != chainID && call.ChainDest.Uint64() == chainID
}

func matchCIRCToDependency(dep CrossRollupDependency, circ *rollupv1.CIRCMessage) bool {
	if circ == nil {
		return false
	}
	if circ.GetLabel() != string(dep.Label) {
		return false
	}
	if recs := circ.GetReceiver(); len(recs) > 0 {
		if len(recs[0]) != common.AddressLength {
			return false
		}
		if common.BytesToAddress(recs[0]) != dep.Receiver {
			return false
		}
	}
	if sid := circ.GetSessionId(); len(sid) > 0 {
		if dep.SessionID == nil {
			return false
		}
		if !bytes.Equal(sid, common.LeftPadBytes(dep.SessionID.Bytes(), 32)) {
			return false
		}
	}
	return true
}
