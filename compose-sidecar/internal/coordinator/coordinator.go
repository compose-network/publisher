package coordinator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/compose-network/compose-sdk/mailbox"
	"github.com/compose-network/compose-sdk/peer"
	"github.com/compose-network/compose-sdk/protocol"
	simsdk "github.com/compose-network/compose-sdk/simulation"
	"github.com/compose-network/compose-sidecar/internal/types"
	"github.com/compose-network/specs/compose/proto"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"
	goproto "google.golang.org/protobuf/proto"
)

const (
	defaultPollInterval = 50 * time.Millisecond
	defaultBuildTimeout = 150 * time.Millisecond
	defaultCIRCTimeout  = 10 * time.Second
	maxResimulations    = 3
)

type DefaultCoordinator struct {
	mu      sync.RWMutex
	log     zerolog.Logger
	chainID uint64

	pending           map[string]*types.PendingXT
	chainStates       map[uint64]*protocol.ChainState
	waiters           map[string]map[uint64]chan *protocol.BuilderPollResponse
	submissionWaiters map[string][]chan string

	simulator       Simulator
	publisher       PublisherClient
	mailboxSender   MailboxSender
	mailboxQueue    mailbox.Queue
	peerCoordinator peer.Coordinator
	putInboxBuilder PutInboxBuilder
	nonceManager    *DeferredNonceManager

	peerVotes map[string]map[uint64]bool

	currentPeriodID      uint64
	currentSuperblockNum uint64

	originSeq uint64

	chainOverlays map[uint64]*chainOverlay
	chainLocks    map[uint64]*sync.Mutex

	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

type CoordinatorConfig struct {
	ChainID         uint64
	Simulator       Simulator
	Publisher       PublisherClient
	MailboxSender   MailboxSender
	MailboxQueue    mailbox.Queue
	PeerCoordinator peer.Coordinator
	PutInboxBuilder PutInboxBuilder
	Log             zerolog.Logger
}

// PutInboxBuilder builds signed putInbox transactions for fulfilled dependencies.
type PutInboxBuilder interface {
	// PendingNonceAt returns the current pending nonce for the coordinator address.
	PendingNonceAt(ctx context.Context) (uint64, error)
	// BuildPutInboxTxWithNonce builds a signed putInbox transaction with the given nonce.
	BuildPutInboxTxWithNonce(
		ctx context.Context,
		dep protocol.CrossRollupDependency,
		nonce uint64,
	) (*ethtypes.Transaction, error)
}

func NewCoordinator(cfg CoordinatorConfig) *DefaultCoordinator {
	return &DefaultCoordinator{
		log:               cfg.Log.With().Str("component", "coordinator").Logger(),
		chainID:           cfg.ChainID,
		pending:           make(map[string]*types.PendingXT),
		chainStates:       make(map[uint64]*protocol.ChainState),
		waiters:           make(map[string]map[uint64]chan *protocol.BuilderPollResponse),
		submissionWaiters: make(map[string][]chan string),
		simulator:         cfg.Simulator,
		publisher:         cfg.Publisher,
		mailboxSender:     cfg.MailboxSender,
		mailboxQueue:      cfg.MailboxQueue,
		peerCoordinator:   cfg.PeerCoordinator,
		putInboxBuilder:   cfg.PutInboxBuilder,
		nonceManager:      NewDeferredNonceManager(),
		peerVotes:         make(map[string]map[uint64]bool),
		chainOverlays:     make(map[uint64]*chainOverlay),
		chainLocks:        make(map[uint64]*sync.Mutex),
		stopCh:            make(chan struct{}),
	}
}

func (c *DefaultCoordinator) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("coordinator already running")
	}

	c.log.Info().Uint64("chain_id", c.chainID).Msg("Starting coordinator")
	c.running = true

	return nil
}

func (c *DefaultCoordinator) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	c.log.Info().Msg("Stopping coordinator")
	close(c.stopCh)
	c.wg.Wait()
	c.running = false

	return nil
}

func (c *DefaultCoordinator) HandleStartPeriod(ctx context.Context, periodID, superblockNum uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentPeriodID = periodID
	c.currentSuperblockNum = superblockNum

	c.log.Info().
		Uint64("period_id", periodID).
		Uint64("superblock_num", superblockNum).
		Msg("Started new period")

	return nil
}

func (c *DefaultCoordinator) HandleStartInstance(ctx context.Context, msg *proto.StartInstance) error {
	if msg == nil {
		return fmt.Errorf("nil StartInstance message")
	}

	instanceID := msg.InstanceIDHex()
	requestKey := xtRequestFingerprint(msg.GetXtRequest())

	c.mu.Lock()
	if _, exists := c.pending[instanceID]; exists {
		c.mu.Unlock()
		return fmt.Errorf("instance %s already pending", instanceID)
	}

	txMap := make(map[uint64][]*ethtypes.Transaction)
	rawTxMap := make(map[uint64][][]byte)

	for _, req := range msg.XtRequest.TransactionRequests {
		chainID := req.ChainId
		for _, txBytes := range req.Transaction {
			tx := new(ethtypes.Transaction)
			if err := tx.UnmarshalBinary(txBytes); err != nil {
				c.mu.Unlock()
				return fmt.Errorf("failed to decode transaction for chain %d: %w", chainID, err)
			}
			txMap[chainID] = append(txMap[chainID], tx)
			rawTxMap[chainID] = append(rawTxMap[chainID], txBytes)
		}
	}

	xt := &types.PendingXT{
		ID:             instanceID,
		InstanceID:     msg.InstanceId,
		PeriodID:       msg.PeriodId,
		SequenceNum:    msg.SequenceNumber,
		Transactions:   txMap,
		RawTxs:         rawTxMap,
		ChainStates:    make(map[uint64]*protocol.ChainState),
		StateOverrides: make(map[uint64]map[string]any),
		CreatedAt:      time.Now(),
		LockedChains:   make(map[uint64]bool),
	}

	c.pending[instanceID] = xt
	c.waiters[instanceID] = make(map[uint64]chan *protocol.BuilderPollResponse)

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

func (c *DefaultCoordinator) HandleBuilderPoll(
	ctx context.Context,
	req *protocol.BuilderPollRequest,
) (*protocol.BuilderPollResponse, error) {
	if req.FlashblockIndex == 0 {
		return &protocol.BuilderPollResponse{Hold: false}, nil
	}

	c.mu.Lock()

	c.nonceManager.ResetForBlock(req.BlockNumber)

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
		Uint64("chain_id", req.ChainID).
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
				xt.LockedChains = make(map[uint64]bool)
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

	// First attempt to deliver any committed prefix.
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
	}, nil
}

type pendingXTEntry struct {
	id string
	xt *types.PendingXT
}

type chainOverlay struct {
	BlockNumber     uint64
	FlashblockIndex uint64
	Overlay         map[string]any
}

type deliverableXT struct {
	id     string
	xt     *types.PendingXT
	rawTxs [][]byte
	deps   []protocol.CrossRollupDependency
}

func xtLess(a, b *pendingXTEntry) bool {
	if a.xt.PeriodID != b.xt.PeriodID {
		return a.xt.PeriodID < b.xt.PeriodID
	}
	if a.xt.SequenceNum != b.xt.SequenceNum {
		return a.xt.SequenceNum < b.xt.SequenceNum
	}
	if a.xt.OriginChain != b.xt.OriginChain {
		return a.xt.OriginChain < b.xt.OriginChain
	}
	if a.xt.OriginSeq != b.xt.OriginSeq {
		return a.xt.OriginSeq < b.xt.OriginSeq
	}
	if !a.xt.CreatedAt.Equal(b.xt.CreatedAt) {
		return a.xt.CreatedAt.Before(b.xt.CreatedAt)
	}
	return a.id < b.id
}

func depsForChain(deps []protocol.CrossRollupDependency, chainID uint64) []protocol.CrossRollupDependency {
	if len(deps) == 0 {
		return nil
	}
	filtered := make([]protocol.CrossRollupDependency, 0, len(deps))
	for _, dep := range deps {
		if dep.DestChainID == chainID {
			filtered = append(filtered, dep)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func (c *DefaultCoordinator) collectDeliverable(
	chainID uint64,
	entries []*pendingXTEntry,
) ([]deliverableXT, *pendingXTEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var deliverable []deliverableXT
	for _, entry := range entries {
		xt := entry.xt
		if xt.DeliveredChains != nil && xt.DeliveredChains[chainID] {
			continue
		}
		if xt.Decision == nil {
			return deliverable, entry
		}
		if !*xt.Decision {
			if xt.DeliveredChains == nil {
				xt.DeliveredChains = make(map[uint64]bool)
			}
			xt.DeliveredChains[chainID] = true
			continue
		}
		rawTxs, ok := xt.RawTxs[chainID]
		if !ok || len(rawTxs) == 0 {
			continue
		}
		deliverable = append(deliverable, deliverableXT{
			id:     entry.id,
			xt:     xt,
			rawTxs: rawTxs,
			deps:   depsForChain(xt.FulfilledDeps, chainID),
		})
	}

	return deliverable, nil
}

func (c *DefaultCoordinator) buildCommittedTransactions(
	ctx context.Context,
	chainID uint64,
	deliverable []deliverableXT,
) ([]protocol.TransactionPayload, error) {
	if len(deliverable) == 0 {
		return nil, nil
	}

	totalDeps := 0
	for _, entry := range deliverable {
		totalDeps += len(entry.deps)
	}

	var nextNonce uint64
	if totalDeps > 0 {
		if c.putInboxBuilder == nil {
			return nil, fmt.Errorf("putInbox builder not configured")
		}
		startNonce, err := c.nonceManager.Reserve(ctx, totalDeps, c.putInboxBuilder.PendingNonceAt)
		if err != nil {
			return nil, err
		}
		nextNonce = startNonce
	}

	txs := make([]protocol.TransactionPayload, 0, len(deliverable)+totalDeps)
	for _, entry := range deliverable {
		for _, dep := range entry.deps {
			putInboxTx, err := c.putInboxBuilder.BuildPutInboxTxWithNonce(ctx, dep, nextNonce)
			if err != nil {
				return nil, err
			}
			nextNonce++
			putInboxBytes, err := putInboxTx.MarshalBinary()
			if err != nil {
				return nil, err
			}
			txs = append(txs, protocol.TransactionPayload{
				Raw:        fmt.Sprintf("0x%x", putInboxBytes),
				Required:   true,
				InstanceID: entry.id,
			})
		}
		for _, rawTx := range entry.rawTxs {
			txs = append(txs, protocol.TransactionPayload{
				Raw:        fmt.Sprintf("0x%x", rawTx),
				Required:   true,
				InstanceID: entry.id,
			})
		}
	}

	return txs, nil
}

func (c *DefaultCoordinator) markDelivered(chainID uint64, deliverable []deliverableXT) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, entry := range deliverable {
		if entry.xt.DeliveredChains == nil {
			entry.xt.DeliveredChains = make(map[uint64]bool)
		}
		entry.xt.DeliveredChains[chainID] = true
	}
}

func (c *DefaultCoordinator) signalWaiters(waiters map[uint64]chan *protocol.BuilderPollResponse) {
	for _, ch := range waiters {
		select {
		case ch <- &protocol.BuilderPollResponse{}:
		default:
		}
		close(ch)
	}
}

func (c *DefaultCoordinator) SubmitXT(ctx context.Context, id string, txs map[uint64][][]byte) (string, error) {
	cleanTxs := make(map[uint64][][]byte)
	for chainID, chainTxs := range txs {
		if len(chainTxs) == 0 {
			continue
		}
		cleanTxs[chainID] = chainTxs
	}
	if len(cleanTxs) == 0 {
		return "", fmt.Errorf("no transactions provided")
	}

	if c.isPublisherConnected() {
		xtRequest := buildXTRequest(cleanTxs)
		requestKey := xtRequestFingerprint(xtRequest)
		waiter := c.registerSubmissionWaiter(requestKey)

		if err := c.sendXTRequestToPublisher(ctx, xtRequest); err != nil {
			c.removeSubmissionWaiter(requestKey, waiter)
			return "", err
		}

		select {
		case instanceID := <-waiter:
			if instanceID == "" {
				return "", fmt.Errorf("publisher returned empty instance_id")
			}
			return instanceID, nil
		case <-ctx.Done():
			c.removeSubmissionWaiter(requestKey, waiter)
			return "", ctx.Err()
		}
	}

	c.mu.Lock()

	if id == "" {
		c.originSeq++
		id = fmt.Sprintf("xt-%d-%d", c.chainID, c.originSeq)
	}

	if _, exists := c.pending[id]; exists {
		c.mu.Unlock()
		return "", fmt.Errorf("XT %s already pending", id)
	}

	txMap := make(map[uint64][]*ethtypes.Transaction)
	for chainID, chainTxs := range cleanTxs {
		for _, txBytes := range chainTxs {
			tx := new(ethtypes.Transaction)
			if err := tx.UnmarshalBinary(txBytes); err != nil {
				c.mu.Unlock()
				return "", fmt.Errorf("failed to decode transaction for chain %d: %w", chainID, err)
			}
			txMap[chainID] = append(txMap[chainID], tx)
		}
	}

	xt := &types.PendingXT{
		ID:             id,
		InstanceID:     []byte(id),
		Transactions:   txMap,
		RawTxs:         cleanTxs,
		ChainStates:    make(map[uint64]*protocol.ChainState),
		StateOverrides: make(map[uint64]map[string]any),
		PeerVotes:      make(map[uint64]bool),
		CreatedAt:      time.Now(),
		OriginChain:    c.chainID,
		OriginSeq:      c.originSeq,
		LockedChains:   make(map[uint64]bool),
	}

	c.pending[id] = xt
	c.waiters[id] = make(map[uint64]chan *protocol.BuilderPollResponse)

	c.log.Info().
		Str("xt_id", id).
		Int("chains", len(txs)).
		Msg("New XT submitted via API")

	c.mu.Unlock()

	if c.peerCoordinator != nil {
		go func() {
			forwardCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := c.peerCoordinator.ForwardXT(forwardCtx, id, txs, c.originSeq); err != nil {
				c.log.Error().Err(err).Str("xt_id", id).Msg("Failed to forward XT to peers")
			} else {
				c.log.Info().Str("xt_id", id).Msg("Forwarded XT to peer sidecars")
			}
		}()
	}

	return id, nil
}

func (c *DefaultCoordinator) sendXTRequestToPublisher(ctx context.Context, xtRequest *proto.XTRequest) error {
	if xtRequest == nil {
		return fmt.Errorf("nil XTRequest")
	}

	c.log.Info().
		Int("chains", len(xtRequest.GetTransactionRequests())).
		Msg("Sending XTRequest to publisher")

	msg := &proto.Message{
		Payload: &proto.Message_XtRequest{XtRequest: xtRequest},
	}

	data, err := goproto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal XTRequest: %w", err)
	}

	if err := c.publisher.SendRaw(ctx, data); err != nil {
		return fmt.Errorf("send XTRequest to publisher: %w", err)
	}

	return nil
}

func buildXTRequest(txs map[uint64][][]byte) *proto.XTRequest {
	if len(txs) == 0 {
		return &proto.XTRequest{}
	}

	chainIDs := make([]uint64, 0, len(txs))
	for chainID := range txs {
		chainIDs = append(chainIDs, chainID)
	}
	sort.Slice(chainIDs, func(i, j int) bool {
		return chainIDs[i] < chainIDs[j]
	})

	txRequests := make([]*proto.TransactionRequest, 0, len(chainIDs))
	for _, chainID := range chainIDs {
		chainTxs := txs[chainID]
		if len(chainTxs) == 0 {
			continue
		}
		txRequests = append(txRequests, &proto.TransactionRequest{
			ChainId:     chainID,
			Transaction: chainTxs,
		})
	}

	return &proto.XTRequest{TransactionRequests: txRequests}
}

func xtRequestFingerprint(req *proto.XTRequest) string {
	if req == nil {
		return ""
	}
	data, err := goproto.Marshal(req)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:16])
}

func (c *DefaultCoordinator) registerSubmissionWaiter(requestKey string) chan string {
	waiter := make(chan string, 1)
	c.mu.Lock()
	c.submissionWaiters[requestKey] = append(c.submissionWaiters[requestKey], waiter)
	c.mu.Unlock()
	return waiter
}

func (c *DefaultCoordinator) removeSubmissionWaiter(requestKey string, waiter chan string) {
	c.mu.Lock()
	waiters := c.submissionWaiters[requestKey]
	for i, ch := range waiters {
		if ch == waiter {
			waiters = append(waiters[:i], waiters[i+1:]...)
			break
		}
	}
	if len(waiters) == 0 {
		delete(c.submissionWaiters, requestKey)
	} else {
		c.submissionWaiters[requestKey] = waiters
	}
	c.mu.Unlock()
}

func (c *DefaultCoordinator) resolveSubmissionWaiter(requestKey, instanceID string) {
	c.mu.Lock()
	waiters := c.submissionWaiters[requestKey]
	if len(waiters) == 0 {
		c.mu.Unlock()
		return
	}
	waiter := waiters[0]
	if len(waiters) == 1 {
		delete(c.submissionWaiters, requestKey)
	} else {
		c.submissionWaiters[requestKey] = waiters[1:]
	}
	c.mu.Unlock()

	select {
	case waiter <- instanceID:
	default:
	}
	close(waiter)
}

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

func (c *DefaultCoordinator) allChainsReady(xt *types.PendingXT) bool {
	for chainID := range xt.Transactions {
		if _, ok := xt.ChainStates[chainID]; !ok {
			return false
		}
	}
	return true
}

func (c *DefaultCoordinator) processXT(ctx context.Context, instanceID string, xt *types.PendingXT) {
	c.log.Info().Str("instance_id", instanceID).Uint64("local_chain", c.chainID).Msg("Processing XT")

	chainLock := c.getChainLock(c.chainID)
	chainLock.Lock()
	defer chainLock.Unlock()

	txBytesList, hasLocalTx := xt.RawTxs[c.chainID]
	if !hasLocalTx || len(txBytesList) == 0 {
		c.log.Debug().
			Str("instance_id", instanceID).
			Uint64("local_chain", c.chainID).
			Msg("No transaction for local chain in this XT")
		// If we don't have a tx for our chain, vote yes (nothing to validate)
		c.sendVote(ctx, instanceID, xt, true)
		return
	}

	// Check chain state is available and lock it for this XT
	c.mu.Lock()
	if xt.LockedChains == nil {
		xt.LockedChains = make(map[uint64]bool)
	}
	xt.LockedChains[c.chainID] = true
	state := xt.ChainStates[c.chainID]
	c.mu.Unlock()
	if state == nil {
		c.log.Error().Uint64("chain_id", c.chainID).Msg("Missing chain state for local chain")
		c.sendVote(ctx, instanceID, xt, false)
		return
	}

	if c.simulator == nil {
		c.log.Warn().Msg("No simulator configured, voting yes without simulation")
		c.sendVote(ctx, instanceID, xt, true)
		return
	}

	// Resolve base state overrides for this chain (builder in-progress state + previous XT overlays)
	baseOverrides := func() map[string]any {
		baseParsed, err := parseStateOverrides(state.StateOverrides)
		if err != nil {
			c.log.Warn().Err(err).Uint64("chain_id", c.chainID).Msg("Failed to parse state_overrides")
		}

		c.mu.Lock()
		defer c.mu.Unlock()
		overlay := c.chainOverlays[c.chainID]
		if overlay == nil || overlay.BlockNumber != state.BlockNumber ||
			overlay.FlashblockIndex != state.FlashblockIndex {
			overlay = &chainOverlay{
				BlockNumber:     state.BlockNumber,
				FlashblockIndex: state.FlashblockIndex,
				Overlay:         cloneStateOverrides(baseParsed),
			}
			c.chainOverlays[c.chainID] = overlay
		}
		return cloneStateOverrides(overlay.Overlay)
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
				c.log.Error().Err(err).Uint64("chain_id", c.chainID).Msg("Simulation failed")
				c.sendVote(ctx, instanceID, xt, false)
				return
			}

			newDeps := make([]protocol.CrossRollupDependency, 0, len(result.Dependencies))
			for _, dep := range result.Dependencies {
				key := depKey(dep)
				if _, ok := depKeys[key]; !ok {
					depKeys[key] = struct{}{}
					allDeps = append(allDeps, dep)
				}
				if _, ok := fulfilledKeys[key]; !ok {
					newDeps = append(newDeps, dep)
				}
			}

			for _, msg := range result.OutboundMessages {
				if containsMessage(allSentMsgs, msg) {
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
					Uint64("chain_id", c.chainID).
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
					key := depKey(dep)
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
				delta := cloneStateOverrides(result.StateOverrides)
				xtOverrides = simsdk.MergeStateOverrides(xtOverrides, cloneStateOverrides(delta))
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
			xt.StateOverrides = make(map[uint64]map[string]any)
		}
		xt.StateOverrides[c.chainID] = xtOverrides
	}
	c.mu.Unlock()

	c.sendVote(ctx, instanceID, xt, true)
}

func (c *DefaultCoordinator) sendVote(ctx context.Context, instanceID string, xt *types.PendingXT, vote bool) {
	c.mu.Lock()
	xt.SimulatedAt = time.Now()
	xt.VoteSent = true
	xt.LocalVote = &vote
	c.mu.Unlock()

	// v1 mode: send vote to publisher
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

	// v2 standalone mode: exchange votes with peers and make local decision
	c.log.Info().
		Str("instance_id", instanceID).
		Bool("vote", vote).
		Uint64("chain_id", c.chainID).
		Msg("Local vote recorded (v2 standalone mode)")

	// Send vote to peer sidecars
	// Use a background context since the original context may be canceled
	if c.peerCoordinator != nil {
		go func() {
			voteCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := c.peerCoordinator.SendVoteToPeers(voteCtx, instanceID, c.chainID, vote); err != nil {
				c.log.Error().Err(err).Str("instance_id", instanceID).Msg("Failed to send vote to peers")
			}
		}()
	}

	// Check if we can make a decision
	c.tryMakeDecision(ctx, instanceID)
}

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
		baseParsed, err := parseStateOverrides(state.StateOverrides)
		if err != nil {
			c.log.Warn().Err(err).Uint64("chain_id", c.chainID).Msg("Failed to parse state_overrides")
		}
		overlay = &chainOverlay{
			BlockNumber:     state.BlockNumber,
			FlashblockIndex: state.FlashblockIndex,
			Overlay:         cloneStateOverrides(baseParsed),
		}
	}

	overlay.Overlay = simsdk.MergeStateOverrides(overlay.Overlay, overrides)
	c.chainOverlays[c.chainID] = overlay
}

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
		SourceChain:      msg.SourceChainID,
		DestinationChain: msg.DestChainID,
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
		Uint64("source_chain", msg.SourceChainID).
		Uint64("dest_chain", msg.DestChainID).
		Str("label", string(msg.Label)).
		Msg("Sending CIRC message")

	return c.mailboxSender.Send(ctx, msg.DestChainID, protoMsg)
}

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
	timeout := time.After(defaultCIRCTimeout)

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
					Uint64("source_chain", dep.SourceChainID).
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

func (c *DefaultCoordinator) Cleanup(maxAge time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for id, xt := range c.pending {
		if xt.Decision != nil && now.Sub(xt.DecidedAt) > maxAge {
			delete(c.pending, id)
			delete(c.waiters, id)
		}
	}
}

func (c *DefaultCoordinator) HandleMailboxMessage(ctx context.Context, msg *proto.MailboxMessage) error {
	if msg == nil {
		return fmt.Errorf("nil mailbox message")
	}

	c.log.Debug().
		Str("instance_id", string(msg.InstanceId)).
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
	instanceKey := string(msg.InstanceId)
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

func matchesDependency(msg *proto.MailboxMessage, dep protocol.CrossRollupDependency) bool {
	if msg.SourceChain != dep.SourceChainID {
		return false
	}
	if dep.Label != nil && msg.Label != string(dep.Label) {
		return false
	}
	if dep.Sender != (common.Address{}) && common.BytesToAddress(msg.Source) != dep.Sender {
		return false
	}
	return true
}

func containsMessage(msgs []protocol.CrossRollupMessage, msg protocol.CrossRollupMessage) bool {
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

func depKey(dep protocol.CrossRollupDependency) string {
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

func cloneStateOverrides(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	data, err := json.Marshal(src)
	if err != nil {
		return nil
	}
	var dst map[string]any
	if err := json.Unmarshal(data, &dst); err != nil {
		return nil
	}
	return dst
}

func parseStateOverrides(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func (c *DefaultCoordinator) getChainLock(chainID uint64) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	lock, ok := c.chainLocks[chainID]
	if !ok {
		lock = &sync.Mutex{}
		c.chainLocks[chainID] = lock
	}
	return lock
}

// isPublisherConnected returns whether the publisher client is connected.
func (c *DefaultCoordinator) isPublisherConnected() bool {
	return c.publisher != nil && c.publisher.IsConnected()
}

// tryMakeDecision attempts to make a decision for an XT in v2 standalone mode.
// Called after receiving a vote (local or from peer).
func (c *DefaultCoordinator) tryMakeDecision(ctx context.Context, instanceID string) {
	c.mu.Lock()
	xt, exists := c.pending[instanceID]
	if !exists {
		c.mu.Unlock()
		return
	}

	// Already decided
	if xt.Decision != nil {
		c.mu.Unlock()
		return
	}

	// Count expected votes (chains that have transactions)
	expectedVotes := len(xt.RawTxs)

	// Count collected votes
	collectedVotes := 0
	allYes := true

	// Check local vote
	if xt.LocalVote != nil {
		collectedVotes++
		if !*xt.LocalVote {
			allYes = false
		}
	}

	// Check peer votes
	if xt.PeerVotes == nil {
		xt.PeerVotes = make(map[uint64]bool)
	}
	for chainID := range xt.RawTxs {
		if chainID == c.chainID {
			continue // Already counted local vote
		}
		if vote, ok := xt.PeerVotes[chainID]; ok {
			collectedVotes++
			if !vote {
				allYes = false
			}
		}
	}

	c.log.Debug().
		Str("instance_id", instanceID).
		Int("expected", expectedVotes).
		Int("collected", collectedVotes).
		Bool("all_yes", allYes).
		Msg("Checking decision status")

	// Not all votes collected yet
	if collectedVotes < expectedVotes {
		c.mu.Unlock()
		return
	}

	// Make decision: COMMIT if all voted yes, ABORT otherwise
	decision := allYes
	xt.Decision = &decision
	xt.DecidedAt = time.Now()

	waiters := c.waiters[instanceID]
	c.mu.Unlock()

	c.applyCommittedOverrides(instanceID)

	c.log.Info().
		Str("instance_id", instanceID).
		Bool("decision", decision).
		Int("votes", collectedVotes).
		Msg("Made local decision (v2 standalone mode)")

	// Notify waiters
	c.signalWaiters(waiters)
	c.mu.Lock()
	delete(c.waiters, instanceID)
	c.mu.Unlock()
}

// HandlePeerVote processes a vote received from a peer sidecar.
func (c *DefaultCoordinator) HandlePeerVote(ctx context.Context, instanceID string, chainID uint64, vote bool) error {
	c.mu.Lock()
	xt, exists := c.pending[instanceID]
	if !exists {
		c.mu.Unlock()
		return fmt.Errorf("unknown instance: %s", instanceID)
	}

	if xt.PeerVotes == nil {
		xt.PeerVotes = make(map[uint64]bool)
	}
	xt.PeerVotes[chainID] = vote
	c.mu.Unlock()

	c.log.Info().
		Str("instance_id", instanceID).
		Uint64("peer_chain", chainID).
		Bool("vote", vote).
		Msg("Received peer vote")

	// Try to make decision with the new vote
	c.tryMakeDecision(ctx, instanceID)

	return nil
}

// HandleForwardedXT processes an XT forwarded from another sidecar.
func (c *DefaultCoordinator) HandleForwardedXT(
	ctx context.Context,
	instanceID string,
	txs map[uint64][][]byte,
	originChain uint64,
	originSeq uint64,
) error {
	c.mu.Lock()

	if instanceID == "" {
		c.mu.Unlock()
		return fmt.Errorf("missing instance_id for forwarded XT")
	}

	if _, exists := c.pending[instanceID]; exists {
		c.mu.Unlock()
		// Already have this XT, ignore duplicate
		return nil
	}

	cleanTxs := make(map[uint64][][]byte)
	txMap := make(map[uint64][]*ethtypes.Transaction)
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
		ChainStates:    make(map[uint64]*protocol.ChainState),
		StateOverrides: make(map[uint64]map[string]any),
		PeerVotes:      make(map[uint64]bool),
		CreatedAt:      time.Now(),
		OriginChain:    originChain,
		OriginSeq:      originSeq,
		LockedChains:   make(map[uint64]bool),
	}

	c.pending[instanceID] = xt
	c.waiters[instanceID] = make(map[uint64]chan *protocol.BuilderPollResponse)

	c.log.Info().
		Str("xt_id", instanceID).
		Int("chains", len(txs)).
		Uint64("origin_chain", xt.OriginChain).
		Uint64("origin_seq", xt.OriginSeq).
		Msg("Received forwarded XT from peer")

	c.mu.Unlock()

	requestKey := xtRequestFingerprint(buildXTRequest(cleanTxs))
	c.resolveSubmissionWaiter(requestKey, instanceID)

	return nil
}

// GetXTStatus retrieves the current status of a cross-chain transaction.
func (c *DefaultCoordinator) GetXTStatus(ctx context.Context, instanceID string) (*XTStatusResponse, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	xt, exists := c.pending[instanceID]
	if !exists {
		return nil, fmt.Errorf("XT not found: %s", instanceID)
	}

	status := c.determineXTStatus(xt)

	return &XTStatusResponse{
		InstanceID: instanceID,
		Status:     status,
		Decision:   xt.Decision,
	}, nil
}

// determineXTStatus determines the current status of a PendingXT based on its state.
func (c *DefaultCoordinator) determineXTStatus(xt *types.PendingXT) protocol.XTStatus {
	// Final states
	if xt.Decision != nil {
		if *xt.Decision {
			return protocol.XTStatusCommitted
		}
		return protocol.XTStatusAborted
	}

	// Vote has been sent but no decision yet
	if xt.VoteSent {
		return protocol.XTStatusVoted
	}

	// Has been simulated but not yet voted
	if !xt.SimulatedAt.IsZero() {
		// Check if waiting for CIRC messages
		if len(xt.Dependencies) > 0 && len(xt.FulfilledDeps) < len(xt.Dependencies) {
			return protocol.XTStatusWaitingCIRC
		}
		return protocol.XTStatusSimulated
	}

	// Currently being simulated (has chain states but not yet completed simulation)
	if len(xt.ChainStates) > 0 {
		return protocol.XTStatusSimulating
	}

	// Initial state
	return protocol.XTStatusPending
}
