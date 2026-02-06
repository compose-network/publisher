package mailbox

import (
	"github.com/compose-network/compose-sdk/protocol"
	"github.com/compose-network/specs/compose/proto"
	"github.com/ethereum/go-ethereum/common"
)

// MatchesDependency checks whether a MailboxMessage satisfies a CrossRollupDependency.
// All non-zero dependency fields must match for a positive result. Zero-value fields
// are treated as wildcards.
func MatchesDependency(msg *proto.MailboxMessage, dep protocol.CrossRollupDependency) bool {
	if msg.SourceChain != uint64(dep.SourceChainID) {
		return false
	}
	if dep.DestChainID != 0 && msg.DestinationChain != uint64(dep.DestChainID) {
		return false
	}
	if dep.Sender != (common.Address{}) && common.BytesToAddress(msg.Source) != dep.Sender {
		return false
	}
	if dep.Receiver != (common.Address{}) && common.BytesToAddress(msg.Receiver) != dep.Receiver {
		return false
	}
	if dep.SessionID != nil && dep.SessionID.Sign() > 0 && msg.SessionId != dep.SessionID.Uint64() {
		return false
	}
	if len(dep.Label) > 0 && msg.Label != string(dep.Label) {
		return false
	}
	return true
}
