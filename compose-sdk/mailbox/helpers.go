package mailbox

import (
	"encoding/hex"
	"fmt"

	"github.com/compose-network/compose-sdk/protocol"
)

// ContainsMessage checks whether a CrossRollupMessage already exists in a slice,
// matching on source/dest chain, sender, receiver, and label.
func ContainsMessage(msgs []protocol.CrossRollupMessage, msg protocol.CrossRollupMessage) bool {
	for _, m := range msgs {
		if m.SourceChainID == msg.SourceChainID &&
			m.DestChainID == msg.DestChainID &&
			m.Sender == msg.Sender &&
			m.Receiver == msg.Receiver &&
			string(m.Label) == string(msg.Label) {
			return true
		}
	}
	return false
}

// DepKey returns a unique string key for a CrossRollupDependency, suitable for
// deduplication in maps.
func DepKey(dep protocol.CrossRollupDependency) string {
	label := ""
	if len(dep.Label) > 0 {
		label = hex.EncodeToString(dep.Label)
	}
	session := ""
	if dep.SessionID != nil {
		session = dep.SessionID.String()
	}
	return fmt.Sprintf(
		"%d|%d|%s|%s|%s|%s|%t|%t",
		dep.SourceChainID,
		dep.DestChainID,
		dep.Sender.Hex(),
		dep.Receiver.Hex(),
		session,
		label,
		dep.RequiredData,
		dep.IsInboxRead,
	)
}
