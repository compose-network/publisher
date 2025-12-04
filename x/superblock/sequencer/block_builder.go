package sequencer

import (
	"fmt"
	"sync"
	"time"

	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/rs/zerolog"
)

type DraftBlock struct {
	slot            uint64
	blockNumber     uint64
	localTxs        [][]byte
	scpTxs          map[string][][]byte
	mailboxMessages []*pb.MailboxMessage
	timestamp       time.Time
}

type BlockBuilder struct {
	mu      sync.RWMutex
	chainID compose.ChainID
	log     zerolog.Logger

	// Current draft
	draft *DraftBlock
}

func NewBlockBuilder(chainID compose.ChainID, log zerolog.Logger) *BlockBuilder {
	return &BlockBuilder{
		chainID: chainID,
		log:     log.With().Str("component", "block_builder").Logger(),
	}
}

func (bb *BlockBuilder) AddLocalTransaction(tx []byte) error {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	if bb.draft == nil {
		return fmt.Errorf("no active draft block")
	}

	bb.draft.localTxs = append(bb.draft.localTxs, tx)
	bb.log.Debug().Int("tx_size", len(tx)).Msg("Added local transaction")
	return nil
}

// AddSCPTransactions adds or removes SCP-related transaction(s) for a given xtID
// If decision is true, provided txs are appended; if false, any existing entries are removed.
func (bb *BlockBuilder) AddSCPTransactions(xtID string, txs [][]byte, decision bool) error {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	if bb.draft == nil {
		return fmt.Errorf("no active draft block")
	}

	if decision {
		if len(txs) > 0 {
			bb.draft.scpTxs[xtID] = append(bb.draft.scpTxs[xtID], txs...)
		}
		bb.log.Info().Str("xt_id", xtID).Int("txs", len(txs)).Msg("Added SCP transactions (commit)")
	} else {
		delete(bb.draft.scpTxs, xtID)
		bb.log.Info().Str("xt_id", xtID).Msg("Removed SCP transaction (abort)")
	}

	return nil
}

func (bb *BlockBuilder) AddMailboxMessage(msg *pb.MailboxMessage) error {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	if bb.draft == nil {
		return fmt.Errorf("no active draft block")
	}

	bb.draft.mailboxMessages = append(bb.draft.mailboxMessages, msg)
	bb.log.Debug().
		Str("instance_id", string(msg.InstanceId)).
		Str("label", msg.Label).
		Msg("Added CIRC message to draft")

	return nil
}

func (bb *BlockBuilder) GetDraftStats() map[string]interface{} {
	bb.mu.RLock()
	defer bb.mu.RUnlock()

	if bb.draft == nil {
		return map[string]interface{}{"active": false}
	}

	return map[string]interface{}{
		"active":        true,
		"slot":          bb.draft.slot,
		"block_number":  bb.draft.blockNumber,
		"local_txs":     len(bb.draft.localTxs),
		"scp_txs":       len(bb.draft.scpTxs),
		"circ_messages": len(bb.draft.mailboxMessages),
		"age_seconds":   time.Since(bb.draft.timestamp).Seconds(),
	}
}

func (bb *BlockBuilder) Reset() {
	bb.mu.Lock()
	defer bb.mu.Unlock()

	bb.draft = nil
	bb.log.Debug().Msg("Block builder reset")
}
