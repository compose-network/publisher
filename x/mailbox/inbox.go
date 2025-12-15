package mailbox

import (
	"context"
	"fmt"
	"time"

	rollupv1 "github.com/compose-network/publisher/proto/rollup/v1"
	spconsensus "github.com/compose-network/publisher/x/consensus"
	"github.com/rs/zerolog"
)

type circQueue interface {
	ConsumeCIRCMessage(xtID *rollupv1.XtID, sourceChainID string) (*rollupv1.CIRCMessage, error)
	RecordCIRCMessage(circMessage *rollupv1.CIRCMessage) error
	GetState(xtID *rollupv1.XtID) (*spconsensus.TwoPCState, bool)
}

type consensusInbox struct {
	queue circQueue
	log   zerolog.Logger
}

func newConsensusInbox(consensus circQueue, log zerolog.Logger) MessageInbox {
	if consensus == nil {
		return nil
	}
	return &consensusInbox{queue: consensus, log: log}
}

func (c *consensusInbox) WaitForDependency(
	ctx context.Context,
	xtID *rollupv1.XtID,
	dep CrossRollupDependency,
	waitCfg WaitConfig,
) (*rollupv1.CIRCMessage, error) {
	sourceKey := spconsensus.ChainKeyUint64(dep.SourceChainID)
	timeoutCtx, cancel := context.WithTimeout(ctx, waitCfg.Timeout)
	defer cancel()

	ticker := time.NewTicker(waitCfg.PollInterval)
	defer ticker.Stop()

	attempt := 0

	for {
		select {
		case <-timeoutCtx.Done():
			return nil, c.logCIRCTimeout(xtID, dep)
		case <-ticker.C:
			attempt++
			circMsg, err := c.queue.ConsumeCIRCMessage(xtID, sourceKey)
			if err != nil {
				c.logCIRCWait(xtID, sourceKey, waitCfg, attempt, err)
				continue
			}

			if matchCIRCToDependency(dep, circMsg) {
				c.log.Info().
					Str("from", sourceKey).
					Str("label", circMsg.GetLabel()).
					Int("data_len", func() int {
						if len(circMsg.Data) == 0 {
							return 0
						}
						return len(circMsg.Data[0])
					}()).
					Msg("Consumed matching CIRC message")
				return circMsg, nil
			}

			if err := c.queue.RecordCIRCMessage(circMsg); err != nil {
				c.log.Warn().Err(err).Msg("Failed to re-queue non-matching CIRC message")
			} else {
				c.log.Info().
					Str("from", sourceKey).
					Str("label", circMsg.GetLabel()).
					Msg("Deferred non-matching CIRC message")
			}
		}
	}
}

func (c *consensusInbox) logCIRCWait(
	xtID *rollupv1.XtID,
	sourceChainID string,
	waitCfg WaitConfig,
	attempt int,
	waitErr error,
) {
	if attempt%10 != 0 {
		return
	}
	remaining := waitCfg.Timeout - time.Duration(attempt)*waitCfg.PollInterval
	c.log.Info().
		Str("xt_id", xtID.Hex()).
		Str("from", sourceChainID).
		Int64("wait_ms", remaining.Milliseconds()).
		Str("err", waitErr.Error()).
		Msg("Still waiting for CIRC message")
}

func (c *consensusInbox) logCIRCTimeout(xtID *rollupv1.XtID, dep CrossRollupDependency) error {
	if c.queue != nil {
		if st, ok := c.queue.GetState(xtID); ok && st != nil {
			counts := make(map[string]int)
			for k, v := range st.CIRCMessages {
				counts[k] = len(v)
			}
			c.log.Warn().
				Str("xt_id", xtID.Hex()).
				Uint64("src_chain", dep.SourceChainID).
				Uint64("dest_chain", dep.DestChainID).
				Str("sender", dep.Sender.Hex()).
				Str("session_id", dep.SessionID.String()).
				Str("label", string(dep.Label)).
				Interface("queues", counts).
				Msg("Timeout waiting for CIRC message")
		}
	}
	return fmt.Errorf(
		"timeout waiting for CIRC message: read(chainMessageSender=%d, sender=%s, sessionId=%s, label=%s) on dest chain %d",
		dep.SourceChainID,
		dep.Sender.Hex(),
		dep.SessionID.String(),
		string(dep.Label),
		dep.DestChainID,
	)
}
