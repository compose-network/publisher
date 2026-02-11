package coordinator

import (
	"context"
	"fmt"
	"time"

	"github.com/compose-network/compose-sdk/mailbox"
	"github.com/compose-network/compose-sdk/protocol"
	simsdk "github.com/compose-network/compose-sdk/simulation"
	"github.com/compose-network/compose-sidecar/internal/types"
	"github.com/compose-network/specs/compose"
	"github.com/compose-network/specs/compose/proto"
)

// Simulator defines the interface for transaction simulation.
type Simulator interface {
	Simulate(
		ctx context.Context,
		chainID compose.ChainID,
		tx []byte,
		stateOverrides map[string]interface{},
	) (*protocol.SimulationResult, error)

	SimulateWithMailbox(
		ctx context.Context,
		chainID compose.ChainID,
		tx []byte,
		stateOverrides map[string]interface{},
		alreadySentMsgs []protocol.CrossRollupMessage,
		fulfilledDeps []protocol.CrossRollupDependency,
	) (*protocol.SimulationResult, error)
}

// MailboxSender sends mailbox messages to peer sidecars.
type MailboxSender interface {
	Send(ctx context.Context, destChainID compose.ChainID, msg *proto.MailboxMessage) error
}

// processXT runs the simulation pipeline for the local chain's portion of an XT.
// It simulates transactions sequentially, discovers mailbox dependencies, waits
// for CIRC messages, and sends a vote.
func (c *DefaultCoordinator) processXT(ctx context.Context, instanceID string, xt *types.PendingXT) {
	c.log.Info().Str("instance_id", instanceID).Uint64("local_chain", uint64(c.chainID)).Msg("Processing XT")

	chainLock := c.getChainLock(c.chainID)
	chainLock.Lock()
	defer chainLock.Unlock()

	txBytesList, hasLocalTx := xt.RawTxs[c.chainID]
	if !hasLocalTx || len(txBytesList) == 0 {
		c.log.Warn().
			Str("instance_id", instanceID).
			Uint64("local_chain", uint64(c.chainID)).
			Msg("Received StartInstance but local chain has no transactions, rejecting")
		c.sendVote(ctx, instanceID, xt, false)
		return
	}

	c.mu.Lock()
	if xt.LockedChains == nil {
		xt.LockedChains = make(map[compose.ChainID]bool)
	}
	xt.LockedChains[c.chainID] = true
	state := xt.ChainStates[c.chainID]
	c.mu.Unlock()
	if state == nil {
		c.log.Error().Uint64("chain_id", uint64(c.chainID)).Msg("Missing chain state for local chain")
		c.sendVote(ctx, instanceID, xt, false)
		return
	}

	if c.simulator == nil {
		c.log.Warn().Msg("No simulator configured, voting yes without simulation")
		c.sendVote(ctx, instanceID, xt, true)
		return
	}

	baseOverrides := func() map[string]any {
		baseParsed, err := simsdk.ParseStateOverrides(state.StateOverrides)
		if err != nil {
			c.log.Warn().Err(err).Uint64("chain_id", uint64(c.chainID)).Msg("Failed to parse state_overrides")
		}

		c.mu.Lock()
		defer c.mu.Unlock()
		overlay := c.chainOverlays[c.chainID]
		if overlay == nil || overlay.BlockNumber != state.BlockNumber ||
			overlay.FlashblockIndex != state.FlashblockIndex {
			overlay = &chainOverlay{
				BlockNumber:     state.BlockNumber,
				FlashblockIndex: state.FlashblockIndex,
				Overlay:         simsdk.CloneStateOverrides(baseParsed),
			}
			c.chainOverlays[c.chainID] = overlay
		}
		return simsdk.CloneStateOverrides(overlay.Overlay)
	}()

	currentOverrides := baseOverrides
	var xtOverrides map[string]any
	allSentMsgs := make([]protocol.CrossRollupMessage, 0)
	allDeps := make([]protocol.CrossRollupDependency, 0)
	allFulfilledDeps := make([]protocol.CrossRollupDependency, 0)
	depKeys := make(map[string]struct{})
	fulfilledKeys := make(map[string]struct{})

	for txIndex, txBytes := range txBytesList {
		success := false
		for attempt := 0; attempt < maxResimulations; attempt++ {
			result, err := c.simulator.SimulateWithMailbox(
				ctx,
				c.chainID,
				txBytes,
				currentOverrides,
				allSentMsgs,
				allFulfilledDeps,
			)
			if err != nil {
				c.log.Error().Err(err).Uint64("chain_id", uint64(c.chainID)).Msg("Simulation failed")
				c.sendVote(ctx, instanceID, xt, false)
				return
			}

			newDeps := make([]protocol.CrossRollupDependency, 0, len(result.Dependencies))
			for _, dep := range result.Dependencies {
				key := mailbox.DepKey(dep)
				if _, ok := depKeys[key]; !ok {
					depKeys[key] = struct{}{}
					allDeps = append(allDeps, dep)
				}
				if _, ok := fulfilledKeys[key]; !ok {
					newDeps = append(newDeps, dep)
				}
			}

			for _, msg := range result.OutboundMessages {
				if mailbox.ContainsMessage(allSentMsgs, msg) {
					continue
				}
				if err := c.sendCIRCMessage(ctx, instanceID, xt.InstanceID, &msg); err != nil {
					c.log.Error().Err(err).Str("instance_id", instanceID).Msg("Failed to send CIRC message")
				}
				allSentMsgs = append(allSentMsgs, msg)
			}

			if !result.Success && len(result.Dependencies) == 0 {
				txs := xt.Transactions[c.chainID]
				txHash := ""
				if txIndex < len(txs) && txs[txIndex] != nil {
					txHash = txs[txIndex].Hash().Hex()
				}
				c.log.Warn().
					Uint64("chain_id", uint64(c.chainID)).
					Str("error", result.Error).
					Str("tx_hash", txHash).
					Int("tx_index", txIndex).
					Msg("Simulation returned failure with no dependencies")
				c.sendVote(ctx, instanceID, xt, false)
				return
			}

			if len(newDeps) > 0 {
				c.log.Debug().
					Str("instance_id", instanceID).
					Int("dependencies", len(newDeps)).
					Int("tx_index", txIndex).
					Msg("Waiting for dependencies")

				fulfilled, err := c.waitForDependencies(ctx, instanceID, xt, newDeps)
				if err != nil {
					c.log.Error().Err(err).Str("instance_id", instanceID).Msg("Failed to fulfill dependencies")
					c.sendVote(ctx, instanceID, xt, false)
					return
				}
				for _, dep := range fulfilled {
					key := mailbox.DepKey(dep)
					if _, ok := fulfilledKeys[key]; ok {
						continue
					}
					fulfilledKeys[key] = struct{}{}
					allFulfilledDeps = append(allFulfilledDeps, dep)
				}
				continue
			}

			if !result.Success {
				c.log.Warn().
					Str("instance_id", instanceID).
					Str("error", result.Error).
					Int("attempt", attempt+1).
					Int("tx_index", txIndex).
					Msg("Simulation still failing after dependencies")
				c.sendVote(ctx, instanceID, xt, false)
				return
			}

			if result.StateOverrides != nil {
				delta := simsdk.CloneStateOverrides(result.StateOverrides)
				xtOverrides = simsdk.MergeStateOverrides(xtOverrides, simsdk.CloneStateOverrides(delta))
				currentOverrides = simsdk.MergeStateOverrides(currentOverrides, delta)
			}

			success = true
			break
		}

		if !success {
			c.log.Warn().
				Str("instance_id", instanceID).
				Int("tx_index", txIndex).
				Msg("Simulation failed after max attempts")
			c.sendVote(ctx, instanceID, xt, false)
			return
		}
	}

	c.mu.Lock()
	if len(allDeps) > 0 {
		xt.Dependencies = allDeps
	}
	if len(allSentMsgs) > 0 {
		xt.OutboundMessages = allSentMsgs
	}
	if len(allFulfilledDeps) > 0 {
		xt.FulfilledDeps = allFulfilledDeps
	}
	if xtOverrides != nil {
		if xt.StateOverrides == nil {
			xt.StateOverrides = make(map[compose.ChainID]map[string]any)
		}
		xt.StateOverrides[c.chainID] = xtOverrides
	}
	c.mu.Unlock()

	c.sendVote(ctx, instanceID, xt, true)
}

// sendVote records and transmits the local vote for an instance.
func (c *DefaultCoordinator) sendVote(ctx context.Context, instanceID string, xt *types.PendingXT, vote bool) {
	c.mu.Lock()
	xt.SimulatedAt = time.Now()
	xt.VoteSent = true
	xt.LocalVote = &vote
	c.mu.Unlock()

	if c.isPublisherConnected() {
		if err := c.publisher.SendVote(ctx, xt.InstanceID, vote); err != nil {
			c.log.Error().Err(err).Str("instance_id", instanceID).Msg("Failed to send vote to publisher")
		}
		c.log.Info().
			Str("instance_id", instanceID).
			Bool("vote", vote).
			Msg("Vote sent to publisher")
		return
	}

	c.log.Info().
		Str("instance_id", instanceID).
		Bool("vote", vote).
		Uint64("chain_id", uint64(c.chainID)).
		Msg("Local vote recorded (standalone mode)")

	if c.peerCoordinator != nil {
		peerCtx := ctx
		if peerCtx == nil {
			peerCtx = context.Background()
		}
		go func() {
			voteCtx, cancel := context.WithTimeout(context.WithoutCancel(peerCtx), 5*time.Second)
			defer cancel()
			if err := c.peerCoordinator.SendVoteToPeers(voteCtx, instanceID, c.chainID, vote); err != nil {
				c.log.Error().Err(err).Str("instance_id", instanceID).Msg("Failed to send vote to peers")
			}
		}()
	}

	c.tryMakeDecision(ctx, instanceID)
}

// applyCommittedOverrides merges a committed XT's state changes into the
// chain overlay so subsequent simulations see the correct base state.
func (c *DefaultCoordinator) applyCommittedOverrides(instanceID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	xt, exists := c.pending[instanceID]
	if !exists || xt.Decision == nil || !*xt.Decision {
		return
	}

	overrides := xt.StateOverrides[c.chainID]
	state := xt.ChainStates[c.chainID]
	if overrides == nil || state == nil {
		return
	}

	overlay := c.chainOverlays[c.chainID]
	if overlay == nil || overlay.BlockNumber != state.BlockNumber || overlay.FlashblockIndex != state.FlashblockIndex {
		baseParsed, err := simsdk.ParseStateOverrides(state.StateOverrides)
		if err != nil {
			c.log.Warn().Err(err).Uint64("chain_id", uint64(c.chainID)).Msg("Failed to parse state_overrides")
		}
		overlay = &chainOverlay{
			BlockNumber:     state.BlockNumber,
			FlashblockIndex: state.FlashblockIndex,
			Overlay:         simsdk.CloneStateOverrides(baseParsed),
		}
	}

	overlay.Overlay = simsdk.MergeStateOverrides(overlay.Overlay, overrides)
	c.chainOverlays[c.chainID] = overlay
}

// sendCIRCMessage sends a mailbox (CIRC) message to the destination chain's sidecar.
func (c *DefaultCoordinator) sendCIRCMessage(
	ctx context.Context,
	instanceID string,
	instanceIDBytes []byte,
	msg *protocol.CrossRollupMessage,
) error {
	if c.mailboxSender == nil {
		return fmt.Errorf("mailbox sender not configured")
	}

	protoMsg := &proto.MailboxMessage{
		InstanceId:       instanceIDBytes,
		SourceChain:      uint64(msg.SourceChainID),
		DestinationChain: uint64(msg.DestChainID),
		Source:           msg.Sender.Bytes(),
		Receiver:         msg.Receiver.Bytes(),
		Label:            string(msg.Label),
		Data:             [][]byte{msg.Data},
	}
	if msg.SessionID != nil {
		protoMsg.SessionId = msg.SessionID.Uint64()
	}

	c.log.Debug().
		Str("instance_id", instanceID).
		Uint64("source_chain", uint64(msg.SourceChainID)).
		Uint64("dest_chain", uint64(msg.DestChainID)).
		Str("label", string(msg.Label)).
		Msg("Sending CIRC message")

	return c.mailboxSender.Send(ctx, msg.DestChainID, protoMsg)
}

// waitForDependencies polls the XT's pending mailbox until all requested
// dependencies are fulfilled or the CIRC timeout expires.
func (c *DefaultCoordinator) waitForDependencies(
	ctx context.Context,
	instanceID string,
	xt *types.PendingXT,
	deps []protocol.CrossRollupDependency,
) ([]protocol.CrossRollupDependency, error) {
	if c.mailboxQueue == nil {
		return nil, fmt.Errorf("mailbox queue not configured")
	}

	fulfilled := make([]protocol.CrossRollupDependency, 0, len(deps))
	timeout := time.After(c.circTimeout)

	for _, dep := range deps {
		for {
			c.mu.RLock()
			pendingMsgs := xt.PendingMailbox
			c.mu.RUnlock()

			var foundMsg *proto.MailboxMessage
			for _, msg := range pendingMsgs {
				if matchesDependency(msg, dep) {
					foundMsg = msg
					break
				}
			}

			if foundMsg != nil {
				dep.Data = nil
				if len(foundMsg.Data) > 0 {
					dep.Data = foundMsg.Data[0]
				}
				fulfilled = append(fulfilled, dep)
				break
			}

			select {
			case <-timeout:
				c.log.Warn().
					Str("instance_id", instanceID).
					Uint64("source_chain", uint64(dep.SourceChainID)).
					Str("label", string(dep.Label)).
					Msg("Timeout waiting for CIRC dependency")
				return nil, fmt.Errorf("timeout waiting for CIRC from chain %d", dep.SourceChainID)
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
	}

	return fulfilled, nil
}
