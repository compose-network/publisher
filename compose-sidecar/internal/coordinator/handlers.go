package coordinator

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/compose-network/compose-sdk/mailbox"
	"github.com/compose-network/compose-sdk/protocol"
	"github.com/compose-network/compose-sidecar/internal/types"
	"github.com/compose-network/specs/compose"
	"github.com/compose-network/specs/compose/proto"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// HandleStartPeriod processes a new period from the publisher, aborting any
// stale undecided instances from prior periods.
func (c *DefaultCoordinator) HandleStartPeriod(ctx context.Context, periodID compose.PeriodID, superblockNum compose.SuperblockNumber) error {
	c.mu.Lock()

	var staleWaiters []map[compose.ChainID]chan *protocol.BuilderPollResponse
	aborted := 0
	for id, xt := range c.pending {
		if xt.Decision != nil || xt.PeriodID == 0 || xt.PeriodID >= periodID {
			continue
		}
		decision := false
		xt.Decision = &decision
		xt.DecidedAt = time.Now()
		if w, ok := c.waiters[id]; ok {
			staleWaiters = append(staleWaiters, w)
			delete(c.waiters, id)
		}
		aborted++
	}

	c.currentPeriodID = periodID
	c.currentSuperblockNum = superblockNum
	c.periodInitialized = true
	c.lastSequenceNum = 0
	c.resetBlockTrackingLocked()

	c.mu.Unlock()

	for _, w := range staleWaiters {
		c.signalWaiters(w)
	}

	c.log.Info().
		Uint64("period_id", uint64(periodID)).
		Uint64("superblock_num", uint64(superblockNum)).
		Int("aborted_stale", aborted).
		Msg("Started new period")

	return nil
}

// HandleRollback aborts all undecided instances and resets period state.
func (c *DefaultCoordinator) HandleRollback(ctx context.Context, periodID compose.PeriodID, lastFinalizedSuperblockNum uint64, lastFinalizedSuperblockHash []byte) error {
	c.mu.Lock()

	var allWaiters []map[compose.ChainID]chan *protocol.BuilderPollResponse
	aborted := 0
	for id, xt := range c.pending {
		if xt.Decision != nil {
			continue
		}
		decision := false
		xt.Decision = &decision
		xt.DecidedAt = time.Now()
		if w, ok := c.waiters[id]; ok {
			allWaiters = append(allWaiters, w)
			delete(c.waiters, id)
		}
		aborted++
	}

	c.periodInitialized = false
	c.lastSequenceNum = 0
	c.resetBlockTrackingLocked()

	c.mu.Unlock()

	for _, w := range allWaiters {
		c.signalWaiters(w)
	}

	c.log.Warn().
		Uint64("period_id", uint64(periodID)).
		Uint64("last_finalized_superblock", lastFinalizedSuperblockNum).
		Int("aborted_instances", aborted).
		Msg("Rollback received, all undecided instances aborted")

	return nil
}

// HandleStartInstance processes a new instance from the publisher. It validates
// the period and sequence, decodes transactions, and registers the XT.
func (c *DefaultCoordinator) HandleStartInstance(ctx context.Context, msg *proto.StartInstance) error {
	if msg == nil {
		return fmt.Errorf("nil StartInstance message")
	}

	instanceID := msg.InstanceIDHex()
	requestKey := xtRequestFingerprint(msg.GetXtRequest())

	includesLocal := false
	for _, req := range msg.XtRequest.TransactionRequests {
		if compose.ChainID(req.ChainId) == c.chainID && len(req.Transaction) > 0 {
			includesLocal = true
			break
		}
	}

	txMap := make(map[compose.ChainID][]*ethtypes.Transaction)
	rawTxMap := make(map[compose.ChainID][][]byte)
	var decodeErr error

	for _, req := range msg.XtRequest.TransactionRequests {
		chainID := compose.ChainID(req.ChainId)
		for _, txBytes := range req.Transaction {
			tx := new(ethtypes.Transaction)
			if err := tx.UnmarshalBinary(txBytes); err != nil {
				decodeErr = fmt.Errorf("failed to decode transaction for chain %d: %w", chainID, err)
				break
			}
			txMap[chainID] = append(txMap[chainID], tx)
			rawTxMap[chainID] = append(rawTxMap[chainID], txBytes)
		}
		if decodeErr != nil {
			break
		}
	}

	c.mu.Lock()
	if _, exists := c.pending[instanceID]; exists {
		c.mu.Unlock()
		return fmt.Errorf("instance %s already pending", instanceID)
	}
	if !c.periodInitialized {
		c.mu.Unlock()
		c.rejectStartInstance(ctx, msg, "period not initialized")
		return nil
	}
	if compose.PeriodID(msg.PeriodId) != c.currentPeriodID {
		c.mu.Unlock()
		c.rejectStartInstance(ctx, msg, "period mismatch")
		return nil
	}
	if compose.SequenceNumber(msg.SequenceNumber) <= c.lastSequenceNum {
		c.mu.Unlock()
		c.rejectStartInstance(ctx, msg, "stale sequence number")
		return nil
	}
	if includesLocal && c.hasActiveInstanceLocked() {
		c.lastSequenceNum = compose.SequenceNumber(msg.SequenceNumber)
		c.mu.Unlock()
		c.rejectStartInstance(ctx, msg, "active instance in progress")
		return nil
	}
	if decodeErr != nil {
		c.lastSequenceNum = compose.SequenceNumber(msg.SequenceNumber)
		c.mu.Unlock()
		c.rejectStartInstance(ctx, msg, decodeErr.Error())
		return nil
	}
	c.lastSequenceNum = compose.SequenceNumber(msg.SequenceNumber)

	xt := &types.PendingXT{
		ID:             instanceID,
		InstanceID:     msg.InstanceId,
		PeriodID:       compose.PeriodID(msg.PeriodId),
		SequenceNum:    compose.SequenceNumber(msg.SequenceNumber),
		Transactions:   txMap,
		RawTxs:         rawTxMap,
		ChainStates:    make(map[compose.ChainID]*protocol.ChainState),
		StateOverrides: make(map[compose.ChainID]map[string]any),
		CreatedAt:      time.Now(),
		LockedChains:   make(map[compose.ChainID]bool),
	}

	c.pending[instanceID] = xt
	c.waiters[instanceID] = make(map[compose.ChainID]chan *protocol.BuilderPollResponse)

	c.log.Info().
		Str("instance_id", instanceID).
		Uint64("period_id", msg.PeriodId).
		Uint64("sequence", msg.SequenceNumber).
		Int("chains", len(txMap)).
		Msg("New instance started")

	c.mu.Unlock()

	c.resolveSubmissionWaiter(requestKey, instanceID)

	return nil
}

// HandleBuilderPoll processes a poll from op-rbuilder, returning committed
// transactions or a hold signal if an undecided XT is blocking.
func (c *DefaultCoordinator) HandleBuilderPoll(
	ctx context.Context,
	req *protocol.BuilderPollRequest,
) (*protocol.BuilderPollResponse, error) {
	if req.FlashblockIndex == 0 {
		return &protocol.BuilderPollResponse{Hold: false}, nil
	}

	c.mu.Lock()

	c.nonceManager.ResetForBlock(req.BlockNumber)

	if prev, ok := c.lastKnownBlocks[req.ChainID]; ok && req.BlockNumber < prev {
		c.log.Warn().
			Uint64("chain_id", uint64(req.ChainID)).
			Uint64("reported_block", req.BlockNumber).
			Uint64("last_known_block", prev).
			Msg("Builder reported a block number lower than previously seen")
	}
	c.lastKnownBlocks[req.ChainID] = req.BlockNumber

	state := &protocol.ChainState{
		ChainID:         req.ChainID,
		BlockNumber:     req.BlockNumber,
		FlashblockIndex: req.FlashblockIndex,
		StateRoot:       req.StateRoot,
		Timestamp:       req.Timestamp,
		GasLimit:        req.GasLimit,
		ReceivedAt:      time.Now(),
		StateOverrides:  req.StateOverrides,
	}
	c.chainStates[req.ChainID] = state

	c.log.Debug().
		Uint64("chain_id", uint64(req.ChainID)).
		Uint64("block", req.BlockNumber).
		Uint64("flashblock", req.FlashblockIndex).
		Msg("Builder poll received")

	var entries []*pendingXTEntry

	for id, xt := range c.pending {
		_, needsChain := xt.Transactions[req.ChainID]
		if !needsChain {
			continue
		}

		entry := &pendingXTEntry{id: id, xt: xt}
		entries = append(entries, entry)
		if xt.Decision == nil {
			if xt.LockedChains == nil {
				xt.LockedChains = make(map[compose.ChainID]bool)
			}
			if !xt.LockedChains[req.ChainID] {
				xt.ChainStates[req.ChainID] = state
			}
		}
	}

	var xtsToProcess []*pendingXTEntry
	var firstUndecided *pendingXTEntry
	for _, entry := range entries {
		if entry.xt.Decision != nil {
			continue
		}
		if firstUndecided == nil || xtLess(entry, firstUndecided) {
			firstUndecided = entry
		}
	}
	if firstUndecided != nil {
		ready := false
		if c.peerCoordinator != nil {
			_, ready = firstUndecided.xt.ChainStates[c.chainID]
		} else if c.isPublisherConnected() {
			ready = c.allChainsReady(firstUndecided.xt)
		} else {
			_, ready = firstUndecided.xt.ChainStates[c.chainID]
		}
		if ready && !firstUndecided.xt.VoteSent {
			firstUndecided.xt.VoteSent = true
			xtsToProcess = append(xtsToProcess, firstUndecided)
		}
	}
	c.mu.Unlock()

	for _, entry := range xtsToProcess {
		go c.processXT(context.Background(), entry.id, entry.xt)
	}

	if len(entries) == 0 {
		return &protocol.BuilderPollResponse{Hold: false}, nil
	}

	sort.Slice(entries, func(i, j int) bool {
		return xtLess(entries[i], entries[j])
	})

	deliverable, blocking := c.collectDeliverable(req.ChainID, entries)
	if len(deliverable) > 0 {
		buildCtx, cancel := context.WithTimeout(ctx, defaultBuildTimeout)
		defer cancel()
		txs, err := c.buildCommittedTransactions(buildCtx, req.ChainID, deliverable)
		if err != nil {
			c.log.Error().Err(err).Msg("Failed to build putInbox transactions")
			return &protocol.BuilderPollResponse{
				Hold:        true,
				PollAfterMs: uint64(defaultPollInterval.Milliseconds()),
				MaxHoldMs:   uint64(c.circTimeout.Milliseconds()),
			}, nil
		}
		c.markDelivered(req.ChainID, deliverable)
		return &protocol.BuilderPollResponse{Hold: false, Txs: txs}, nil
	}

	if blocking == nil {
		return &protocol.BuilderPollResponse{Hold: false}, nil
	}

	return &protocol.BuilderPollResponse{
		Hold:        true,
		PollAfterMs: uint64(defaultPollInterval.Milliseconds()),
		MaxHoldMs:   uint64(c.circTimeout.Milliseconds()),
	}, nil
}

// HandleMailboxMessage handles an incoming CIRC message from a peer sidecar.
func (c *DefaultCoordinator) HandleMailboxMessage(ctx context.Context, msg *proto.MailboxMessage) error {
	if msg == nil {
		return fmt.Errorf("nil mailbox message")
	}

	instanceKey := hex.EncodeToString(msg.InstanceId)

	c.log.Debug().
		Str("instance_id", instanceKey).
		Uint64("source_chain", msg.SourceChain).
		Uint64("dest_chain", msg.DestinationChain).
		Str("label", msg.Label).
		Msg("Received mailbox message from peer")

	if c.mailboxQueue != nil {
		if err := c.mailboxQueue.Record(msg); err != nil {
			return fmt.Errorf("record mailbox message: %w", err)
		}
	}

	c.mu.Lock()
	if xt, ok := c.pending[instanceKey]; ok {
		xt.PendingMailbox = append(xt.PendingMailbox, msg)
		c.log.Debug().
			Str("instance_id", instanceKey).
			Int("pending_count", len(xt.PendingMailbox)).
			Msg("Added mailbox message to pending XT")
	}
	c.mu.Unlock()

	return nil
}

// OnDecision records a commit/abort decision for an instance and notifies
// any blocked builder polls.
func (c *DefaultCoordinator) OnDecision(ctx context.Context, instanceID string, decision bool) error {
	c.mu.Lock()

	xt, exists := c.pending[instanceID]
	if !exists {
		c.mu.Unlock()
		return fmt.Errorf("unknown instance: %s", instanceID)
	}

	xt.Decision = &decision
	xt.DecidedAt = time.Now()

	waiters := c.waiters[instanceID]
	c.mu.Unlock()

	c.applyCommittedOverrides(instanceID)

	c.log.Info().
		Str("instance_id", instanceID).
		Bool("decision", decision).
		Msg("Decision received")

	c.signalWaiters(waiters)
	c.mu.Lock()
	delete(c.waiters, instanceID)
	c.mu.Unlock()

	return nil
}

// HandlePeerVote processes a vote received from a peer sidecar.
func (c *DefaultCoordinator) HandlePeerVote(ctx context.Context, instanceID string, chainID compose.ChainID, vote bool) error {
	c.mu.Lock()
	xt, exists := c.pending[instanceID]
	if !exists {
		c.mu.Unlock()
		return fmt.Errorf("unknown instance: %s", instanceID)
	}

	if xt.PeerVotes == nil {
		xt.PeerVotes = make(map[compose.ChainID]bool)
	}
	xt.PeerVotes[chainID] = vote
	c.mu.Unlock()

	c.log.Info().
		Str("instance_id", instanceID).
		Uint64("peer_chain", uint64(chainID)).
		Bool("vote", vote).
		Msg("Received peer vote")

	c.tryMakeDecision(ctx, instanceID)

	return nil
}

// HandleForwardedXT processes an XT forwarded from another sidecar.
func (c *DefaultCoordinator) HandleForwardedXT(
	ctx context.Context,
	instanceID string,
	txs map[compose.ChainID][][]byte,
	originChain compose.ChainID,
	originSeq compose.SequenceNumber,
) error {
	c.mu.Lock()

	if instanceID == "" {
		c.mu.Unlock()
		return fmt.Errorf("missing instance_id for forwarded XT")
	}

	if _, exists := c.pending[instanceID]; exists {
		c.mu.Unlock()
		return nil
	}

	cleanTxs := make(map[compose.ChainID][][]byte)
	txMap := make(map[compose.ChainID][]*ethtypes.Transaction)
	for chainID, chainTxs := range txs {
		if len(chainTxs) == 0 {
			continue
		}
		cleanTxs[chainID] = chainTxs
		for _, txBytes := range chainTxs {
			tx := new(ethtypes.Transaction)
			if err := tx.UnmarshalBinary(txBytes); err != nil {
				c.mu.Unlock()
				return fmt.Errorf("failed to decode transaction for chain %d: %w", chainID, err)
			}
			txMap[chainID] = append(txMap[chainID], tx)
		}
	}
	if len(cleanTxs) == 0 {
		c.mu.Unlock()
		return fmt.Errorf("forwarded XT has no transactions")
	}

	xt := &types.PendingXT{
		ID:             instanceID,
		InstanceID:     []byte(instanceID),
		Transactions:   txMap,
		RawTxs:         cleanTxs,
		ChainStates:    make(map[compose.ChainID]*protocol.ChainState),
		StateOverrides: make(map[compose.ChainID]map[string]any),
		PeerVotes:      make(map[compose.ChainID]bool),
		CreatedAt:      time.Now(),
		OriginChain:    originChain,
		OriginSeq:      originSeq,
		LockedChains:   make(map[compose.ChainID]bool),
	}

	c.pending[instanceID] = xt
	c.waiters[instanceID] = make(map[compose.ChainID]chan *protocol.BuilderPollResponse)

	c.log.Info().
		Str("xt_id", instanceID).
		Int("chains", len(txs)).
		Uint64("origin_chain", uint64(xt.OriginChain)).
		Uint64("origin_seq", uint64(xt.OriginSeq)).
		Msg("Received forwarded XT from peer")

	c.mu.Unlock()

	requestKey := xtRequestFingerprint(buildXTRequest(cleanTxs))
	c.resolveSubmissionWaiter(requestKey, instanceID)

	return nil
}

// matchesDependency delegates to the SDK mailbox package.
func matchesDependency(msg *proto.MailboxMessage, dep protocol.CrossRollupDependency) bool {
	return mailbox.MatchesDependency(msg, dep)
}
