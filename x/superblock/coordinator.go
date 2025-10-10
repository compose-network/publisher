package superblock

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	pb "github.com/ssvlabs/rollup-shared-publisher/proto/rollup/v1"
	"github.com/ssvlabs/rollup-shared-publisher/x/consensus"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/l1"
	l1events "github.com/ssvlabs/rollup-shared-publisher/x/superblock/l1/events"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/l1/tx"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs"
	apicollector "github.com/ssvlabs/rollup-shared-publisher/x/superblock/proofs/collector"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/queue"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/registry"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/slot"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/store"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/wal"
	"github.com/ssvlabs/rollup-shared-publisher/x/transport"
	"google.golang.org/protobuf/proto"
)

// Coordinator orchestrates the Superblock Construction Protocol (SBCP)
// by managing slot-based execution, cross-chain transactions, and L2 block assembly
type Coordinator struct {
	mu      sync.RWMutex
	config  Config
	log     zerolog.Logger
	metrics prometheus.Registerer

	slotManager     SlotManager
	stateMachine    *slot.StateMachine
	registryService registry.Service
	l2BlockStore    store.L2BlockStore
	superblockStore store.SuperblockStore
	xtQueue         queue.XTRequestQueue
	l1Publisher     l1.Publisher
	walManager      wal.Manager

	consensusCoord consensus.Coordinator
	transport      transport.Server

	proofs *proofPipeline

	running  bool
	stopCh   chan struct{}
	workerWg sync.WaitGroup

	currentExecution *SlotExecution
	stats            map[string]interface{}

	// Cache of recent slot executions for transaction recovery during rollbacks
	executionHistoryMu sync.RWMutex
	executionHistory   map[uint64]*SlotExecution // slot number -> execution snapshot

	l1TrackMu sync.Mutex
	l1Tracked map[uint64][]byte // superblock number -> tx hash
}

func NewCoordinator(
	config Config,
	log zerolog.Logger,
	metrics prometheus.Registerer,
	registryService registry.Service,
	l2BlockStore store.L2BlockStore,
	superblockStore store.SuperblockStore,
	xtQueue queue.XTRequestQueue,
	l1Publisher l1.Publisher,
	walManager wal.Manager,
	consensusCoord consensus.Coordinator,
	transport transport.Server,
	collector apicollector.Service,
	prover proofs.ProverClient,
) *Coordinator {
	slotManagerImpl := slot.NewManager(
		config.Slot.GenesisTime,
		config.Slot.Duration,
		config.Slot.SealCutover,
	)

	stateMachine := slot.NewStateMachine(slotManagerImpl, log)

	c := &Coordinator{
		config:           config,
		log:              log.With().Str("component", "coordinator").Logger(),
		metrics:          metrics,
		slotManager:      slotManagerImpl,
		stateMachine:     stateMachine,
		registryService:  registryService,
		l2BlockStore:     l2BlockStore,
		superblockStore:  superblockStore,
		xtQueue:          xtQueue,
		l1Publisher:      l1Publisher,
		walManager:       walManager,
		consensusCoord:   consensusCoord,
		transport:        transport,
		stopCh:           make(chan struct{}),
		stats:            make(map[string]interface{}),
		l1Tracked:        make(map[uint64][]byte),
		executionHistory: make(map[uint64]*SlotExecution),
	}

	c.setupStateCallbacks()
	c.setupConsensusCallbacks()

	if config.Proofs.Enabled && collector != nil && prover != nil {
		c.proofs = newProofPipeline(
			config.Proofs,
			collector,
			prover,
			superblockStore,
			c.publishWithProof,
			c.log,
		)
	}
	return c
}

func (c *Coordinator) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("coordinator already running")
	}

	c.log.Info().Msg("Starting superblock coordinator")

	if c.walManager != nil {
		if err := c.recoverFromWAL(ctx); err != nil {
			return fmt.Errorf("WAL recovery failed: %w", err)
		}
	}

	c.running = true

	c.workerWg.Add(5)
	go c.slotExecutionLoop(ctx)
	go c.queueProcessor(ctx)
	go c.metricsUpdater(ctx)
	go c.l1EventWatcher(ctx)
	go c.l1ReceiptPoller(ctx)
	if c.proofs != nil {
		c.proofs.Start(ctx)
	}

	return nil
}

func (c *Coordinator) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	c.log.Info().Msg("Stopping superblock coordinator")
	c.running = false

	close(c.stopCh)
	c.workerWg.Wait()

	if c.walManager != nil {
		return c.walManager.Close()
	}

	if c.proofs != nil {
		c.proofs.Stop()
	}

	return nil
}

func (c *Coordinator) GetCurrentSlot() uint64 {
	return c.slotManager.GetCurrentSlot()
}

func (c *Coordinator) GetSlotState() SlotState {
	state := c.stateMachine.GetCurrentState()
	switch state {
	case slot.StateStarting:
		return SlotStateStarting
	case slot.StateFree:
		return SlotStateFree
	case slot.StateLocked:
		return SlotStateLocked
	case slot.StateSealing:
		return SlotStateSealing
	default:
		return SlotStateStarting
	}
}

// recordExecutionSnapshot captures an immutable copy of the execution state so that
// rollback and recovery paths can reconstruct past slots even after process restarts.
func (c *Coordinator) recordExecutionSnapshot(exec *SlotExecution) {
	if exec == nil {
		return
	}

	snapshot := cloneSlotExecution(exec)
	if snapshot == nil {
		return
	}

	c.executionHistoryMu.Lock()
	c.executionHistory[snapshot.Slot] = snapshot
	c.executionHistoryMu.Unlock()

	if c.walManager != nil {
		if data, err := json.Marshal(snapshot); err == nil {
			if err := c.walManager.WriteEntry(context.Background(), &wal.Entry{
				Slot:      snapshot.Slot,
				Type:      wal.EntrySlotSnapshot,
				Data:      data,
				Timestamp: time.Now(),
			}); err != nil {
				c.log.Error().Err(err).Uint64("slot", snapshot.Slot).Msg("Failed to write slot snapshot to WAL")
			}
		} else {
			c.log.Warn().Err(err).Msg("Failed to marshal slot execution snapshot for WAL")
		}
	}

	c.cleanupOldExecutionHistory()
}

// cleanupOldExecutionHistory removes old execution history entries to prevent unbounded memory growth.
// Keeps the most recent 1000 slot executions for rollback recovery.
func (c *Coordinator) cleanupOldExecutionHistory() {
	const maxHistorySize = 1000

	c.executionHistoryMu.Lock()
	defer c.executionHistoryMu.Unlock()

	if len(c.executionHistory) <= maxHistorySize {
		return
	}

	// Collect all slot numbers and sort them
	slots := make([]uint64, 0, len(c.executionHistory))
	for slot := range c.executionHistory {
		slots = append(slots, slot)
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i] < slots[j] })

	// Remove oldest entries to keep only maxHistorySize
	removeCount := len(slots) - maxHistorySize
	for i := 0; i < removeCount; i++ {
		delete(c.executionHistory, slots[i])
	}

	c.log.Debug().
		Int("removed", removeCount).
		Int("remaining", len(c.executionHistory)).
		Msg("Cleaned up old execution history entries")
}

// syncExecutionFromStateMachine refreshes the in-memory execution record based on
// the current slot state machine so rollback snapshots remain accurate.
func (c *Coordinator) syncExecutionFromStateMachine() {
	// Check if currentExecution exists before doing expensive work
	c.mu.RLock()
	if c.currentExecution == nil {
		c.mu.RUnlock()
		return
	}
	c.mu.RUnlock()

	// Gather data from state machine (state machine has its own locks, doesn't need c.mu)
	currentSlot := c.stateMachine.GetCurrentSlot()
	currentState := c.GetSlotState()
	nextSuperblockNumber := c.stateMachine.GetNextSuperblockNumber()
	lastSuperblockHash := append([]byte(nil), c.stateMachine.GetLastSuperblockHash()...)

	// Clone active rollups
	activeRollups := c.stateMachine.GetActiveRollups()
	clonedActiveRollups := make([][]byte, len(activeRollups))
	for i, id := range activeRollups {
		clonedActiveRollups[i] = append([]byte(nil), id...)
	}

	// Clone received L2 blocks (expensive proto.Clone operations done outside lock)
	received := c.stateMachine.GetReceivedL2Blocks()
	clonedL2Blocks := make(map[string]*pb.L2Block, len(received))
	for chainID, block := range received {
		clonedL2Blocks[chainID] = proto.Clone(block).(*pb.L2Block)
	}

	// Clone L2 block requests (expensive proto.Clone operations done outside lock)
	reqs := c.stateMachine.GetL2BlockRequests()
	clonedL2Requests := make(map[string]*pb.L2BlockRequest, len(reqs))
	for chainID, req := range reqs {
		clonedL2Requests[chainID] = proto.Clone(req).(*pb.L2BlockRequest)
	}

	// Clone SCP instances (expensive cloning done outside lock)
	instances := c.stateMachine.GetSCPInstances()
	clonedInstances := make(map[string]*slot.SCPInstance, len(instances))
	for id, inst := range instances {
		clonedInstances[id] = cloneSCPInstance(inst)
	}

	// Now acquire lock and quickly update currentExecution with pre-cloned data
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.currentExecution == nil {
		return
	}

	c.currentExecution.Slot = currentSlot
	c.currentExecution.State = currentState
	c.currentExecution.NextSuperblockNumber = nextSuperblockNumber
	c.currentExecution.LastSuperblockHash = lastSuperblockHash
	c.currentExecution.ActiveRollups = clonedActiveRollups
	c.currentExecution.ReceivedL2Blocks = clonedL2Blocks
	c.currentExecution.L2BlockRequests = clonedL2Requests
	c.currentExecution.SCPInstances = clonedInstances
}

func cloneSCPInstance(inst *slot.SCPInstance) *slot.SCPInstance {
	if inst == nil {
		return nil
	}

	copy := *inst
	copy.XtID = append([]byte(nil), inst.XtID...)
	if len(inst.ParticipatingChains) > 0 {
		copy.ParticipatingChains = make([][]byte, len(inst.ParticipatingChains))
		for i, chain := range inst.ParticipatingChains {
			copy.ParticipatingChains[i] = append([]byte(nil), chain...)
		}
	}
	if len(inst.Votes) > 0 {
		copy.Votes = make(map[string]bool, len(inst.Votes))
		for voter, v := range inst.Votes {
			copy.Votes[voter] = v
		}
	}
	if inst.Request != nil {
		copy.Request = proto.Clone(inst.Request).(*pb.XTRequest)
	}
	if inst.Decision != nil {
		decision := *inst.Decision
		copy.Decision = &decision
	}
	if inst.DecisionTime != nil {
		decisionTime := *inst.DecisionTime
		copy.DecisionTime = &decisionTime
	}
	return &copy
}

func cloneSlotExecution(exec *SlotExecution) *SlotExecution {
	if exec == nil {
		return nil
	}

	clone := &SlotExecution{
		Slot:                 exec.Slot,
		State:                exec.State,
		StartTime:            exec.StartTime,
		NextSuperblockNumber: exec.NextSuperblockNumber,
		LastSuperblockHash:   append([]byte(nil), exec.LastSuperblockHash...),
	}

	if len(exec.ActiveRollups) > 0 {
		clone.ActiveRollups = make([][]byte, len(exec.ActiveRollups))
		for i, chain := range exec.ActiveRollups {
			clone.ActiveRollups[i] = append([]byte(nil), chain...)
		}
	} else {
		clone.ActiveRollups = make([][]byte, 0)
	}

	clone.ReceivedL2Blocks = make(map[string]*pb.L2Block, len(exec.ReceivedL2Blocks))
	for chainID, block := range exec.ReceivedL2Blocks {
		clone.ReceivedL2Blocks[chainID] = proto.Clone(block).(*pb.L2Block)
	}

	clone.SCPInstances = make(map[string]*slot.SCPInstance, len(exec.SCPInstances))
	for id, inst := range exec.SCPInstances {
		clone.SCPInstances[id] = cloneSCPInstance(inst)
	}

	clone.L2BlockRequests = make(map[string]*pb.L2BlockRequest, len(exec.L2BlockRequests))
	for chainID, req := range exec.L2BlockRequests {
		clone.L2BlockRequests[chainID] = proto.Clone(req).(*pb.L2BlockRequest)
	}

	clone.AttemptedRequests = make(map[string]*queue.QueuedXTRequest, len(exec.AttemptedRequests))
	for id, req := range exec.AttemptedRequests {
		reqCopy := *req
		if req.Request != nil {
			reqCopy.Request = proto.Clone(req.Request).(*pb.XTRequest)
		}
		reqCopy.XtID = append([]byte(nil), req.XtID...)
		clone.AttemptedRequests[id] = &reqCopy
	}

	return clone
}

func (c *Coordinator) toSlotStateMachine(state SlotState) slot.State {
	switch state {
	case SlotStateStarting:
		return slot.StateStarting
	case SlotStateFree:
		return slot.StateFree
	case SlotStateLocked:
		return slot.StateLocked
	case SlotStateSealing:
		return slot.StateSealing
	default:
		return slot.StateStarting
	}
}

// Logger exposes the coordinator's logger for external packages (e.g., handlers).
func (c *Coordinator) Logger() *zerolog.Logger { return &c.log }

// Consensus returns the underlying consensus coordinator.
func (c *Coordinator) Consensus() consensus.Coordinator { return c.consensusCoord }

// HandleL2Block is an exported wrapper to process incoming L2 block messages.
func (c *Coordinator) HandleL2Block(ctx context.Context, from string, l2Block *pb.L2Block) error {
	return c.handleL2Block(ctx, from, l2Block)
}

// StateMachine exposes the slot state machine for external observers.
func (c *Coordinator) StateMachine() *slot.StateMachine { return c.stateMachine }

// Transport exposes the transport server for broadcasting messages from adapters.
func (c *Coordinator) Transport() transport.Server { return c.transport }

// StartSCPForAdapter allows adapter layer to initiate SCP with a constructed request.
func (c *Coordinator) StartSCPForAdapter(ctx context.Context, xtReq *pb.XTRequest, xtID []byte, from string) error {
	queuedRequest := &queue.QueuedXTRequest{
		Request:     xtReq,
		XtID:        xtID,
		Priority:    time.Now().Unix(),
		SubmittedAt: time.Now(),
		ExpiresAt:   time.Now().Add(c.config.Queue.RequestExpiration),
		Attempts:    0,
		From:        from,
	}
	return c.startSCP(ctx, queuedRequest)
}

func (c *Coordinator) GetActiveTransactions() []*pb.XtID {
	instances := c.stateMachine.GetSCPInstances()
	var activeXTs []*pb.XtID

	for _, instance := range instances {
		if instance.Decision == nil {
			activeXTs = append(activeXTs, &pb.XtID{
				Hash: instance.XtID,
			})
		}
	}

	return activeXTs
}

func (c *Coordinator) SubmitXTRequest(ctx context.Context, from string, request *pb.XTRequest) error {
	queuedRequest := &queue.QueuedXTRequest{
		Request:     request,
		XtID:        c.calculateXtID(request),
		Priority:    time.Now().Unix(),
		SubmittedAt: time.Now(),
		ExpiresAt:   time.Now().Add(c.config.Queue.RequestExpiration),
		Attempts:    0,
		From:        from,
	}

	return c.xtQueue.Enqueue(ctx, queuedRequest)
}

func (c *Coordinator) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]interface{})
	for k, v := range c.stats {
		result[k] = v
	}

	result["current_slot"] = c.GetCurrentSlot()
	result["slot_state"] = c.GetSlotState().String()
	result["running"] = c.running

	return result
}

func (c *Coordinator) slotExecutionLoop(ctx context.Context) {
	defer c.workerWg.Done()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			if err := c.processSlotTick(ctx); err != nil {
				c.log.Error().Err(err).Msg("Error processing slot tick")
			}
		}
	}
}

func (c *Coordinator) processSlotTick(ctx context.Context) error {
	currentSlot := c.slotManager.GetCurrentSlot()
	currentState := c.stateMachine.GetCurrentState()

	switch currentState {
	case slot.StateStarting:
		return c.handleStartingState(ctx, currentSlot)
	case slot.StateFree:
		return c.handleFreeState(ctx, currentSlot)
	case slot.StateLocked:
		return c.handleLockedState(ctx, currentSlot)
	case slot.StateSealing:
		return c.handleSealingState(ctx, currentSlot)
	}

	return nil
}

func (c *Coordinator) handleStartingState(ctx context.Context, currentSlot uint64) error {
	if currentSlot <= c.stateMachine.GetCurrentSlot() {
		return nil
	}

	activeRollups, err := c.registryService.GetActiveRollups(ctx)
	if err != nil {
		return fmt.Errorf("failed to get active rollups: %w", err)
	}

	lastSuperblock, err := c.superblockStore.GetLatestSuperblock(ctx)
	var nextNumber uint64 = 1
	var lastHash = make([]byte, 32)

	if err == nil && lastSuperblock != nil {
		nextNumber = lastSuperblock.Number + 1
		lastHash = lastSuperblock.Hash.Bytes()
	}

	// Seed state machine with last known L2 heads from store so that
	// L2BlocksRequest reflects actual chain heads (prevents block-number
	// mismatches on first/early slots or after restarts).
	for _, chainID := range activeRollups {
		if latest, err := c.l2BlockStore.GetLatestL2Block(ctx, chainID); err == nil && latest != nil {
			c.stateMachine.SeedLastHead(latest)
		}
	}

	if err := c.stateMachine.BeginSlot(currentSlot, nextNumber, lastHash, activeRollups); err != nil {
		return fmt.Errorf("failed to begin slot: %w", err)
	}

	clonedRollups := make([][]byte, len(activeRollups))
	for i, id := range activeRollups {
		clonedRollups[i] = append([]byte(nil), id...)
	}

	c.mu.Lock()
	c.currentExecution = &SlotExecution{
		Slot:                 currentSlot,
		State:                SlotStateFree,
		StartTime:            time.Now(),
		NextSuperblockNumber: nextNumber,
		LastSuperblockHash:   append([]byte(nil), lastHash...),
		ActiveRollups:        clonedRollups,
		ReceivedL2Blocks:     make(map[string]*pb.L2Block),
		SCPInstances:         make(map[string]*slot.SCPInstance),
		L2BlockRequests:      make(map[string]*pb.L2BlockRequest),
		AttemptedRequests:    make(map[string]*queue.QueuedXTRequest),
	}
	c.mu.Unlock()

	c.syncExecutionFromStateMachine()

	c.recordExecutionSnapshot(c.currentExecution)

	c.sendStartSlotMessages(ctx, currentSlot, nextNumber, lastHash, activeRollups)

	return nil
}

func (c *Coordinator) handleFreeState(ctx context.Context, currentSlot uint64) error {
	if c.slotManager.IsSlotSealTime() {
		return c.requestSeal(ctx, currentSlot)
	}

	queuedRequest, err := c.xtQueue.Peek(ctx)
	if err != nil {
		return err
	}
	if queuedRequest == nil {
		return nil
	}

	if queuedRequest.ExpiresAt.Before(time.Now()) {
		c.xtQueue.Dequeue(ctx)
		c.log.Info().Str("xt_id", fmt.Sprintf("%x", queuedRequest.XtID)).Msg("Expired XT request removed")
		return nil
	}

	return c.startSCP(ctx, queuedRequest)
}

func (c *Coordinator) handleLockedState(ctx context.Context, currentSlot uint64) error {
	if c.slotManager.IsSlotSealTime() {
		return c.requestSeal(ctx, currentSlot)
	}

	return nil
}

func (c *Coordinator) handleSealingState(ctx context.Context, currentSlot uint64) error {
	if c.stateMachine.CheckAllL2BlocksReceived() {
		return c.buildSuperblock(ctx, currentSlot)
	}

	// Check for slot timeout
	stateMachineSlot := c.stateMachine.GetCurrentSlot()
	managerCurrentSlot := c.slotManager.GetCurrentSlot()

	if managerCurrentSlot > stateMachineSlot {
		return c.handleSlotTimeout(ctx, stateMachineSlot)
	}

	return nil
}

func (c *Coordinator) startSCP(ctx context.Context, queuedRequest *queue.QueuedXTRequest) error {
	c.xtQueue.Dequeue(ctx)

	instances := c.stateMachine.GetSCPInstances()
	sequenceNumber := uint64(len(instances))

	participatingChains := c.extractParticipatingChains(queuedRequest.Request)

	scpInstance := &slot.SCPInstance{
		XtID:                queuedRequest.XtID,
		Slot:                c.stateMachine.GetCurrentSlot(),
		SequenceNumber:      sequenceNumber,
		Request:             queuedRequest.Request,
		ParticipatingChains: participatingChains,
		Votes:               make(map[string]bool),
		StartTime:           time.Now(),
	}

	if err := c.stateMachine.StartSCP(scpInstance); err != nil {
		return fmt.Errorf("failed to start SCP: %w", err)
	}

	c.syncExecutionFromStateMachine()

	// Track attempted request for potential requeue on failure path
	c.mu.Lock()
	if c.currentExecution != nil {
		c.currentExecution.AttemptedRequests[fmt.Sprintf("%x", queuedRequest.XtID)] = queuedRequest
	}
	c.mu.Unlock()

	c.recordExecutionSnapshot(c.currentExecution)

	startSCMsg := &pb.Message{
		SenderId: "shared-publisher",
		Payload: &pb.Message_StartSc{
			StartSc: &pb.StartSC{
				Slot:             c.stateMachine.GetCurrentSlot(),
				XtSequenceNumber: sequenceNumber,
				XtRequest:        queuedRequest.Request,
				XtId:             queuedRequest.XtID,
			},
		},
	}

	if err := c.transport.Broadcast(ctx, startSCMsg, ""); err != nil {
		c.log.Error().Err(err).Msg("Failed to broadcast StartSC message")
	}

	c.sendStartSCMessages(ctx, scpInstance)

	c.log.Info().
		Str("xt_id", fmt.Sprintf("%x", scpInstance.XtID)).
		Uint64("sequence", sequenceNumber).
		Int("participating_chains", len(participatingChains)).
		Msg("Started SCP instance")

	return nil
}

func (c *Coordinator) requestSeal(ctx context.Context, currentSlot uint64) error {
	// At seal cutover: abort any in-flight (undecided) xTs and broadcast Decided(false)
	if err := c.forceAbortUndecided(ctx); err != nil {
		return fmt.Errorf("failed to force abort undecided: %w", err)
	}

	includedXTs := c.stateMachine.GetIncludedXTs()

	if err := c.stateMachine.RequestSeal(includedXTs); err != nil {
		return fmt.Errorf("failed to request seal: %w", err)
	}

	c.syncExecutionFromStateMachine()
	c.recordExecutionSnapshot(c.currentExecution)

	c.sendRequestSealMessages(ctx, currentSlot, includedXTs)

	return nil
}

func (c *Coordinator) buildSuperblock(ctx context.Context, slotNumber uint64) error {
	l2Blocks := c.stateMachine.GetReceivedL2Blocks()
	includedXTs := c.stateMachine.GetIncludedXTs()

	if !c.validateL2Blocks(l2Blocks) {
		return c.failSlot(slotNumber, "invalid L2 blocks")
	}

	superblock := &store.Superblock{
		Number:     c.currentExecution.NextSuperblockNumber,
		Slot:       slotNumber,
		ParentHash: common.BytesToHash(c.currentExecution.LastSuperblockHash),
		Timestamp:  time.Now(),
		L2Blocks:   make([]*pb.L2Block, 0, len(l2Blocks)),
		Status:     store.SuperblockStatusPending,
	}

	for _, block := range l2Blocks {
		superblock.L2Blocks = append(superblock.L2Blocks, block)
	}

	superblock.MerkleRoot = common.BytesToHash(c.calculateMerkleRoot(superblock.L2Blocks))
	if len(includedXTs) > 0 {
		sbIDs := make([]common.Hash, 0, len(includedXTs))
		for _, id := range includedXTs {
			sbIDs = append(sbIDs, common.BytesToHash(id))
		}
		superblock.IncludedXTs = sbIDs
	}
	superblock.Hash = common.BytesToHash(c.calculateSuperblockHash(superblock))

	if err := c.superblockStore.StoreSuperblock(ctx, superblock); err != nil {
		return fmt.Errorf("failed to store superblock: %w", err)
	}

	if c.proofs != nil {
		if err := c.proofs.HandleSuperblock(ctx, superblock); err != nil {
			c.log.Warn().Err(err).Uint64("number", superblock.Number).Msg("Failed to enqueue superblock for proving")
		}
		if c.config.Proofs.RequireProof {
			c.log.Info().
				Uint64("number", superblock.Number).
				Uint64("slot", slotNumber).
				Int("l2_blocks", len(superblock.L2Blocks)).
				Int("included_xts", len(includedXTs)).
				Msg("Proof required; deferring L1 publish until proof ready")
			return c.stateMachine.TransitionTo(slot.StateStarting, "proof requested")
		}
	}

	if err := c.publishSuperblockTx(ctx, superblock, nil, nil); err != nil {
		c.log.Error().Err(err).Uint64("number", superblock.Number).Msg("Failed to publish superblock")
	} else {
		c.log.Info().
			Uint64("number", superblock.Number).
			Uint64("slot", slotNumber).
			Int("l2_blocks", len(superblock.L2Blocks)).
			Int("included_xts", len(includedXTs)).
			Msg("Built superblock")
	}

	if err := c.stateMachine.TransitionTo(slot.StateStarting, "superblock built"); err != nil {
		return err
	}

	c.syncExecutionFromStateMachine()
	c.recordExecutionSnapshot(c.currentExecution)

	return nil
}

// l1EventWatcher listens for L1 OutputProposed events and updates status/rollback.
func (c *Coordinator) l1EventWatcher(ctx context.Context) {
	defer c.workerWg.Done()
	if c.l1Publisher == nil {
		return
	}
	ch, err := c.l1Publisher.WatchSuperblocks(ctx)
	if err != nil {
		c.log.Warn().Err(err).Msg("L1 watcher unavailable")
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case ev, ok := <-ch:
			if !ok || ev == nil {
				return
			}
			// Update store for submitted or rollback
			sb, err := c.superblockStore.GetSuperblock(ctx, ev.SuperblockNumber)
			if err != nil {
				// Not ours or not stored; ignore
				continue
			}
			if ev.Type == l1events.SuperblockEventRolledBack {
				sb.Status = store.SuperblockStatusRolledBack
				// Remove from tracking
				c.l1TrackMu.Lock()
				delete(c.l1Tracked, ev.SuperblockNumber)
				c.l1TrackMu.Unlock()

				if err := c.handleSuperblockRollback(ctx, ev, sb); err != nil {
					c.log.Error().
						Err(err).
						Uint64("superblock_number", ev.SuperblockNumber).
						Msg("Rollback handling failed")
				}
			} else if sb.Status == store.SuperblockStatusPending {
				// Ensure at least Submitted
				sb.Status = store.SuperblockStatusSubmitted
			}
			_ = c.superblockStore.StoreSuperblock(ctx, sb)
		}
	}
}

// l1ReceiptPoller periodically checks transaction receipts for tracked superblocks
// and advances their status to Confirmed/Finalized.
func (c *Coordinator) l1ReceiptPoller(ctx context.Context) {
	defer c.workerWg.Done()
	if c.l1Publisher == nil {
		return
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.l1TrackMu.Lock()
			for number, txHash := range c.l1Tracked {
				c.updateStatusFromReceipt(ctx, number, txHash)
			}
			c.l1TrackMu.Unlock()
		}
	}
}

func (c *Coordinator) updateStatusFromReceipt(ctx context.Context, number uint64, txHash []byte) {
	status, err := c.l1Publisher.GetPublishStatus(ctx, txHash)
	if err != nil || status == nil {
		return
	}
	sb, err := c.superblockStore.GetSuperblock(ctx, number)
	if err != nil || sb == nil {
		return
	}
	switch status.Status {
	case tx.TransactionStateFinalized:
		sb.Status = store.SuperblockStatusFinalized
		delete(c.l1Tracked, number)
	case tx.TransactionStateConfirmed:
		sb.Status = store.SuperblockStatusConfirmed
	case tx.TransactionStateIncluded:
		if sb.Status == store.SuperblockStatusPending {
			sb.Status = store.SuperblockStatusSubmitted
		}
	case tx.TransactionStateFailed:
		sb.Status = store.SuperblockStatusPending
		delete(c.l1Tracked, number)
	case tx.TransactionStatePending:
		// no-op
	}
	_ = c.superblockStore.StoreSuperblock(ctx, sb)
}

func (c *Coordinator) setupStateCallbacks() {
	c.stateMachine.RegisterStateChangeCallback(slot.StateFree, c.onStateFree)
	c.stateMachine.RegisterStateChangeCallback(slot.StateLocked, c.onStateLocked)
	c.stateMachine.RegisterStateChangeCallback(slot.StateSealing, c.onStateSealing)
}

func (c *Coordinator) setupConsensusCallbacks() {
	c.consensusCoord.SetStartCallback(c.handleConsensusStart)
	c.consensusCoord.SetVoteCallback(c.handleConsensusVote)
	c.consensusCoord.SetDecisionCallback(c.handleConsensusDecision)
}

func (c *Coordinator) onStateFree(from, to slot.State, slot uint64) {
}

func (c *Coordinator) onStateLocked(from, to slot.State, slot uint64) {
}

func (c *Coordinator) onStateSealing(from, to slot.State, slot uint64) {
}

func (c *Coordinator) handleConsensusStart(ctx context.Context, from string, xtReq *pb.XTRequest) error {
	c.log.Info().
		Str("from", from).
		Str("xt_id", fmt.Sprintf("%x", c.calculateXtID(xtReq))).
		Msg("Consensus start callback")
	return nil
}

func (c *Coordinator) handleConsensusVote(ctx context.Context, xtID *pb.XtID, vote bool) error {
	c.log.Info().Str("xt_id", xtID.Hex()).Bool("vote", vote).Msg("Broadcasting vote to sequencers")

	voteMsg := &pb.Message{
		SenderId: "shared-publisher",
		Payload: &pb.Message_Vote{
			Vote: &pb.Vote{
				SenderChainId: []byte("shared-publisher"),
				XtId:          xtID,
				Vote:          vote,
			},
		},
	}

	return c.transport.Broadcast(ctx, voteMsg, "")
}

func (c *Coordinator) handleConsensusDecision(ctx context.Context, xtID *pb.XtID, decision bool) error {
	c.log.Info().Str("xt_id", xtID.Hex()).Bool("decision", decision).Msg("Broadcasting decision to sequencers")

	decidedMsg := &pb.Message{
		SenderId: "shared-publisher",
		Payload: &pb.Message_Decided{
			Decided: &pb.Decided{
				XtId:     xtID,
				Decision: decision,
			},
		},
	}

	if err := c.transport.Broadcast(ctx, decidedMsg, ""); err != nil {
		return err
	}

	if err := c.stateMachine.ProcessSCPDecision(xtID.Hash, decision); err != nil {
		return err
	}

	c.syncExecutionFromStateMachine()
	c.recordExecutionSnapshot(c.currentExecution)

	return nil
}

// forceAbortUndecided marks undecided SCP instances as decided=false and broadcasts Decided(false)
func (c *Coordinator) forceAbortUndecided(ctx context.Context) error {
	instances := c.stateMachine.GetSCPInstances()
	var errs []error
	for _, inst := range instances {
		if inst.Decision == nil {
			// Broadcast Decided(false)
			decidedMsg := &pb.Message{
				SenderId: "shared-publisher",
				Payload: &pb.Message_Decided{
					Decided: &pb.Decided{XtId: &pb.XtID{Hash: inst.XtID}, Decision: false},
				},
			}
			if err := c.transport.Broadcast(ctx, decidedMsg, ""); err != nil {
				c.log.Error().
					Err(err).
					Str("xt_id", fmt.Sprintf("%x", inst.XtID)).
					Msg("Failed to broadcast forced abort decision")
				errs = append(errs, fmt.Errorf("broadcast forced abort %x: %w", inst.XtID, err))
			}

			// Update state machine
			if err := c.stateMachine.ProcessSCPDecision(inst.XtID, false); err != nil {
				c.log.Error().
					Err(err).
					Str("xt_id", fmt.Sprintf("%x", inst.XtID)).
					Msg("Failed to process forced abort decision")
				errs = append(errs, fmt.Errorf("update state forced abort %x: %w", inst.XtID, err))
			}
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	c.syncExecutionFromStateMachine()
	c.recordExecutionSnapshot(c.currentExecution)

	return nil
}

func (c *Coordinator) queueProcessor(ctx context.Context) {
	defer c.workerWg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			if _, err := c.xtQueue.RemoveExpired(ctx); err != nil {
				c.log.Error().Err(err).Msg("Failed to remove expired requests")
			}
		}
	}
}

func (c *Coordinator) metricsUpdater(ctx context.Context) {
	defer c.workerWg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.updateMetrics(ctx)
		}
	}
}

func (c *Coordinator) updateMetrics(ctx context.Context) {
}

func (c *Coordinator) recoverFromWAL(ctx context.Context) error {
	if c.walManager == nil || !c.config.WAL.Enabled {
		return nil
	}

	entries, err := c.walManager.ReadEntries(ctx, 0)
	if err != nil {
		return fmt.Errorf("read wal entries: %w", err)
	}

	var latest *SlotExecution
	for _, entry := range entries {
		if entry == nil || entry.Type != wal.EntrySlotSnapshot || len(entry.Data) == 0 {
			continue
		}

		var snap SlotExecution
		if err := json.Unmarshal(entry.Data, &snap); err != nil {
			c.log.Warn().Err(err).Msg("Failed to unmarshal WAL snapshot entry")
			continue
		}

		snapshotCopy := cloneSlotExecution(&snap)
		if snapshotCopy != nil {
			latest = snapshotCopy
		}
	}

	if latest == nil {
		return nil
	}

	requests := make([]*pb.L2BlockRequest, 0, len(latest.L2BlockRequests))
	for _, req := range latest.L2BlockRequests {
		requests = append(requests, proto.Clone(req).(*pb.L2BlockRequest))
	}

	c.stateMachine.Reset()
	c.stateMachine.SeedL2BlockRequests(latest.Slot, latest.NextSuperblockNumber, latest.LastSuperblockHash, requests)
	c.stateMachine.RestoreSnapshotState(c.toSlotStateMachine(latest.State), latest.ReceivedL2Blocks, latest.SCPInstances)

	c.executionHistoryMu.Lock()
	c.executionHistory = make(map[uint64]*SlotExecution)
	c.executionHistory[latest.Slot] = cloneSlotExecution(latest)
	c.executionHistoryMu.Unlock()

	c.mu.Lock()
	c.currentExecution = cloneSlotExecution(latest)
	c.mu.Unlock()

	c.syncExecutionFromStateMachine()

	return nil
}

func (c *Coordinator) sendStartSlotMessages(
	ctx context.Context,
	slot uint64,
	superblockNumber uint64,
	lastHash []byte,
	activeRollups [][]byte, //nolint:unparam // later
) {
	l2BlockRequests := c.stateMachine.GetL2BlockRequests()

	blockRequests := make([]*pb.L2BlockRequest, 0, len(l2BlockRequests))
	for _, req := range l2BlockRequests {
		blockRequests = append(blockRequests, req)
	}

	// Log which ChainIDs are being requested for this slot (diagnostics)
	if len(blockRequests) > 0 {
		ids := make([]string, 0, len(blockRequests))
		for _, r := range blockRequests {
			ids = append(ids, fmt.Sprintf("%x", r.ChainId))
		}
		c.log.Info().
			Uint64("slot", slot).
			Int("l2_requests", len(blockRequests)).
			Strs("chain_ids", ids).
			Msg("Broadcasting StartSlot with L2 block requests")
	} else {
		c.log.Info().
			Uint64("slot", slot).
			Msg("Broadcasting StartSlot with no L2 block requests")
	}

	startSlotMsg := &pb.Message{
		SenderId: "shared-publisher",
		Payload: &pb.Message_StartSlot{
			StartSlot: &pb.StartSlot{
				Slot:                 slot,
				NextSuperblockNumber: superblockNumber,
				LastSuperblockHash:   lastHash,
				L2BlocksRequest:      blockRequests,
			},
		},
	}

	if err := c.transport.Broadcast(ctx, startSlotMsg, ""); err != nil {
		c.log.Error().Err(err).Msg("Failed to broadcast StartSlot message")
	}
}

func (c *Coordinator) sendStartSCMessages(ctx context.Context, instance *slot.SCPInstance) {
	xtIDStr := fmt.Sprintf("%x", instance.XtID)

	// Check if transaction already exists in consensus layer
	xtID, err := instance.Request.XtID()
	if err != nil {
		return
	}

	if _, exists := c.consensusCoord.GetState(xtID); exists {
		c.log.Warn().Str("xt_id", xtIDStr).Msg("SCP transaction already exists, skipping duplicate start")
		return
	}

	if err := c.consensusCoord.StartTransaction(ctx, "superblock-coordinator", instance.Request); err != nil {
		c.log.Error().Err(err).Str("xt_id", xtIDStr).Msg("Failed to start SCP transaction")
	}
}

func (c *Coordinator) sendRequestSealMessages(ctx context.Context, slot uint64, includedXTs [][]byte) {
	requestSealMsg := &pb.Message{
		SenderId: "shared-publisher",
		Payload: &pb.Message_RequestSeal{
			RequestSeal: &pb.RequestSeal{
				Slot:        slot,
				IncludedXts: includedXTs,
			},
		},
	}

	if err := c.transport.Broadcast(ctx, requestSealMsg, ""); err != nil {
		c.log.Error().Err(err).Msg("Failed to broadcast RequestSeal message")
	}
}

func (c *Coordinator) extractParticipatingChains(request *pb.XTRequest) [][]byte {
	chains := make([][]byte, 0, len(request.Transactions))
	for _, tx := range request.Transactions {
		chains = append(chains, tx.ChainId)
	}
	return chains
}

func (c *Coordinator) validateL2Blocks(blocks map[string]*pb.L2Block) bool {
	slot := c.stateMachine.GetCurrentSlot()
	reqs := c.stateMachine.GetL2BlockRequests()

	for chainIDStr, blk := range blocks {
		if blk.Slot != slot {
			c.log.Error().Uint64("expected_slot", slot).Uint64("got", blk.Slot).Msg("L2 block with wrong slot")
			return false
		}
		req, ok := reqs[chainIDStr]
		if !ok {
			c.log.Error().Str("chain", fmt.Sprintf("%x", blk.ChainId)).Msg("unexpected chain in L2 blocks")
			return false
		}
		// accept blocks that are at or ahead of the requested number.
		// Reject only if the sequencer submitted an older block than requested.
		// TODO: rethink, parent hash validation too
		if blk.BlockNumber < req.BlockNumber {
			c.log.Error().
				Uint64("expected_min", req.BlockNumber).
				Uint64("got", blk.BlockNumber).
				Msg("L2 block number below requested minimum")
			return false
		}
	}

	return true
}

func (c *Coordinator) failSlot(slotNumber uint64, reason string) error {
	c.log.Error().Uint64("slot", slotNumber).Str("reason", reason).Msg("Slot failed")
	// Requeue attempted xTs for next slot
	if err := c.requeueAttemptedRequests(context.Background()); err != nil {
		c.log.Error().Err(err).Msg("Failed to requeue attempted requests")
	}

	if err := c.stateMachine.TransitionTo(slot.StateStarting, fmt.Sprintf("slot failed: %s", reason)); err != nil {
		return err
	}

	c.syncExecutionFromStateMachine()
	c.recordExecutionSnapshot(c.currentExecution)

	return nil
}

// requeueAttemptedRequests requeues all attempted xTs for the next slot
func (c *Coordinator) requeueAttemptedRequests(ctx context.Context) error {
	c.mu.Lock()
	if c.currentExecution == nil || len(c.currentExecution.AttemptedRequests) == 0 {
		c.mu.Unlock()
		return nil
	}
	reqs := make([]*queue.QueuedXTRequest, 0, len(c.currentExecution.AttemptedRequests))
	for _, r := range c.currentExecution.AttemptedRequests {
		reqs = append(reqs, r)
	}
	c.currentExecution.AttemptedRequests = make(map[string]*queue.QueuedXTRequest)
	c.mu.Unlock()

	return c.xtQueue.RequeueForSlot(ctx, reqs)
}

func (c *Coordinator) handleSlotTimeout(ctx context.Context, slotNumber uint64) error {
	c.log.Warn().Uint64("slot", slotNumber).Msg("Slot timeout")

	// attempt to build superblock with partial blocks
	return c.buildPartialSuperblock(ctx, slotNumber)
}

func (c *Coordinator) buildPartialSuperblock(ctx context.Context, slotNumber uint64) error {
	receivedL2Blocks := c.stateMachine.GetReceivedL2Blocks()
	scpInstances := c.stateMachine.GetSCPInstances()

	// check if we can build with partial blocks
	receivedChainIDs := make(map[string]bool)
	for chainID := range receivedL2Blocks {
		receivedChainIDs[chainID] = true
	}

	// Check if SCPs with decisions=1 have all their participating chains represented
	canBuildPartial := true
	for _, instance := range scpInstances {
		if instance.Decision != nil && *instance.Decision {
			// This SCP was included, check if all its chains sent blocks
			hasAtLeastOneChain := false
			for _, chainID := range instance.ParticipatingChains {
				if receivedChainIDs[string(chainID)] {
					hasAtLeastOneChain = true
					break
				}
			}
			if !hasAtLeastOneChain {
				canBuildPartial = false
				break
			}
		}
	}

	if canBuildPartial && c.validateL2Blocks(receivedL2Blocks) {
		c.log.Info().
			Uint64("slot", slotNumber).
			Int("received_blocks", len(receivedL2Blocks)).
			Int("total_rollups", len(c.stateMachine.GetActiveRollups())).
			Msg("Building partial superblock due to timeout")
		return c.buildSuperblock(ctx, slotNumber)
	} else {
		c.log.Warn().
			Uint64("slot", slotNumber).
			Int("received_blocks", len(receivedL2Blocks)).
			Bool("can_build_partial", canBuildPartial).
			Msg("Cannot build superblock, failing slot")
		return c.failSlot(slotNumber, "timeout with insufficient blocks")
	}
}

func (c *Coordinator) calculateXtID(request *pb.XTRequest) []byte {
	xtID, _ := request.XtID()
	return xtID.Hash
}

func (c *Coordinator) publishSuperblockTx(
	ctx context.Context,
	sb *store.Superblock,
	proof []byte,
	outputs *proofs.SuperblockAggOutputs,
) error {
	if len(proof) == 0 {
		return fmt.Errorf("proof is required for superblock submission")
	}

	recorded, err := c.l1Publisher.PublishSuperblockWithProof(ctx, sb, proof, outputs)
	if err != nil {
		return err
	}

	sb.Status = store.SuperblockStatusSubmitted
	sb.L1TransactionHash = common.BytesToHash(recorded.Hash)
	if err := c.superblockStore.StoreSuperblock(ctx, sb); err != nil {
		c.log.Warn().Err(err).Uint64("number", sb.Number).Msg("Failed to persist superblock post-publish")
	}

	c.l1TrackMu.Lock()
	c.l1Tracked[sb.Number] = append([]byte(nil), recorded.Hash...)
	c.l1TrackMu.Unlock()

	return nil
}

func (c *Coordinator) publishWithProof(
	ctx context.Context,
	sb *store.Superblock,
	proof []byte,
	outputs *proofs.SuperblockAggOutputs,
) error {
	return c.publishSuperblockTx(ctx, sb, proof, outputs)
}

func (c *Coordinator) calculateMerkleRoot(blocks []*pb.L2Block) []byte {
	if len(blocks) == 0 {
		return make([]byte, 32)
	}
	// Deterministic leaf order: sort by chainID (lexicographic)
	sort.Slice(blocks, func(i, j int) bool { return string(blocks[i].ChainId) < string(blocks[j].ChainId) })

	// Compute leaf hashes = keccak256(chainID || blockHash || blockNumberBE)
	leaves := make([][]byte, len(blocks))
	for i, b := range blocks {
		buf := make([]byte, 0, len(b.ChainId)+len(b.BlockHash)+8)
		buf = append(buf, b.ChainId...)
		buf = append(buf, b.BlockHash...)
		num := make([]byte, 8)
		binary.BigEndian.PutUint64(num, b.BlockNumber)
		buf = append(buf, num...)
		h := crypto.Keccak256(buf)
		leaves[i] = h
	}

	// Build Merkle tree with keccak256(left||right), duplicate last when odd
	level := leaves
	for len(level) > 1 {
		var next [][]byte
		for i := 0; i < len(level); i += 2 {
			left := level[i]
			right := left
			if i+1 < len(level) {
				right = level[i+1]
			}
			combined := append(append([]byte{}, left...), right...)
			next = append(next, crypto.Keccak256(combined))
		}
		level = next
	}
	return level[0]
}

func (c *Coordinator) calculateSuperblockHash(superblock *store.Superblock) []byte {
	// Header fields: Number || Slot || ParentHash || MerkleRoot
	header := make([]byte, 0, 8+8+common.HashLength+common.HashLength)
	nb := make([]byte, 8)
	sb := make([]byte, 8)
	binary.BigEndian.PutUint64(nb, superblock.Number)
	binary.BigEndian.PutUint64(sb, superblock.Slot)
	header = append(header, nb...)
	header = append(header, sb...)
	header = append(header, superblock.ParentHash.Bytes()...)
	header = append(header, superblock.MerkleRoot.Bytes()...)
	return crypto.Keccak256(header)
}

func (c *Coordinator) handleL2Block(ctx context.Context, from string, l2Block *pb.L2Block) error {
	c.log.Info().
		Str("from", from).
		Uint64("block_number", l2Block.BlockNumber).
		Str("chain_id", fmt.Sprintf("%x", l2Block.ChainId)).
		Msg("Received L2 block")

	if err := c.l2BlockStore.StoreL2Block(ctx, l2Block); err != nil {
		c.log.Error().Err(err).Msg("Failed to store L2 block")
		return err
	}

	if err := c.stateMachine.ReceiveL2Block(l2Block); err != nil {
		return err
	}

	c.syncExecutionFromStateMachine()
	c.recordExecutionSnapshot(c.currentExecution)

	return nil
}

// handleSuperblockRollback is triggered when L1 reports that a previously proposed superblock was rolled back.
// The full recovery flow will be built incrementally; for now, guard against invalid events and surface a clear error.
//
// //nolint:gocyclo // dev
func (c *Coordinator) handleSuperblockRollback(
	ctx context.Context,
	ev *l1events.SuperblockEvent,
	rolledBack *store.Superblock,
) error {
	if rolledBack == nil {
		return fmt.Errorf("rolled-back superblock data missing for event %d", ev.SuperblockNumber)
	}
	if ev.SuperblockNumber == 0 {
		return fmt.Errorf("rollback event for genesis superblock is invalid")
	}
	if rolledBack.Number != ev.SuperblockNumber {
		return fmt.Errorf("rolled-back superblock mismatch: event=%d stored=%d", ev.SuperblockNumber, rolledBack.Number)
	}

	lastValid, err := c.findLastValidSuperblock(ctx, rolledBack.Number)
	if err != nil {
		return fmt.Errorf("failed to locate last valid superblock: %w", err)
	}

	l2Requests, err := c.computeL2BlockRequestsAfterRollback(ctx, lastValid)
	if err != nil {
		return fmt.Errorf("failed to compute L2 block requests for rollback: %w", err)
	}

	if err := c.requeueRolledBackTransactions(ctx, rolledBack.Slot); err != nil {
		c.log.Warn().Err(err).Uint64("slot", rolledBack.Slot).Msg("Failed to requeue transactions from rolled-back slot")
	}

	nextSuperblockNumber := uint64(1)
	lastHash := make([]byte, 32)
	if lastValid != nil {
		nextSuperblockNumber = lastValid.Number + 1
		lastHash = lastValid.Hash.Bytes()
	}

	currentSlot := c.slotManager.GetCurrentSlot()
	if currentSlot == 0 {
		currentSlot = c.stateMachine.GetCurrentSlot()
	}
	if currentSlot == 0 {
		currentSlot = rolledBack.Slot + 1
	}

	if err := c.sendRollBackAndStartSlot(ctx, currentSlot, nextSuperblockNumber, lastHash, l2Requests); err != nil {
		return fmt.Errorf("failed to broadcast rollback message: %w", err)
	}

	if lastValid != nil {
		for _, block := range lastValid.L2Blocks {
			c.stateMachine.SeedLastHead(block)
		}
	}

	c.stateMachine.Reset()
	c.stateMachine.SeedL2BlockRequests(currentSlot, nextSuperblockNumber, lastHash, l2Requests)

	// Create fresh execution context aligned with the rollback restart parameters.
	activeRollups := make([][]byte, 0, len(l2Requests))
	for _, req := range l2Requests {
		activeRollups = append(activeRollups, append([]byte(nil), req.ChainId...))
	}

	l2BlockRequestMap := make(map[string]*pb.L2BlockRequest, len(l2Requests))
	for _, req := range l2Requests {
		l2BlockRequestMap[string(req.ChainId)] = proto.Clone(req).(*pb.L2BlockRequest)
	}

	c.mu.Lock()
	c.currentExecution = &SlotExecution{
		Slot:                 currentSlot,
		State:                SlotStateStarting,
		StartTime:            time.Now(),
		NextSuperblockNumber: nextSuperblockNumber,
		LastSuperblockHash:   append([]byte(nil), lastHash...),
		ActiveRollups:        activeRollups,
		ReceivedL2Blocks:     make(map[string]*pb.L2Block),
		SCPInstances:         make(map[string]*slot.SCPInstance),
		L2BlockRequests:      l2BlockRequestMap,
		AttemptedRequests:    make(map[string]*queue.QueuedXTRequest),
	}
	c.mu.Unlock()

	c.syncExecutionFromStateMachine()
	c.recordExecutionSnapshot(c.currentExecution)

	c.executionHistoryMu.Lock()
	for slot := range c.executionHistory {
		if slot >= rolledBack.Slot {
			delete(c.executionHistory, slot)
		}
	}
	c.executionHistoryMu.Unlock()

	c.mu.Lock()
	c.currentExecution = nil
	c.mu.Unlock()

	c.log.Info().
		Uint64("rolled_back_number", rolledBack.Number).
		Uint64("last_valid_number", func() uint64 {
			if lastValid == nil {
				return 0
			}
			return lastValid.Number
		}()).
		Uint64("next_superblock_number", nextSuperblockNumber).
		Uint64("restart_slot", currentSlot).
		Int("l2_requests", len(l2Requests)).
		Msg("Handled superblock rollback; restart instructions broadcast")

	return nil
}

func (c *Coordinator) findLastValidSuperblock(ctx context.Context, rolledBackNumber uint64) (*store.Superblock, error) {
	if rolledBackNumber == 0 {
		return nil, fmt.Errorf("invalid rolled-back number 0")
	}

	for number := rolledBackNumber - 1; number > 0; number-- {
		sb, err := c.superblockStore.GetSuperblock(ctx, number)
		if err != nil {
			continue
		}
		if sb.Status != store.SuperblockStatusRolledBack {
			return sb, nil
		}
	}

	return nil, nil
}

func (c *Coordinator) computeL2BlockRequestsAfterRollback(
	ctx context.Context,
	lastValid *store.Superblock,
) ([]*pb.L2BlockRequest, error) {
	activeRollups, err := c.registryService.GetActiveRollups(ctx)
	if err != nil {
		return nil, err
	}

	requests := make([]*pb.L2BlockRequest, 0, len(activeRollups))
	for _, chainID := range activeRollups {
		var headBlock *pb.L2Block

		if lastValid != nil {
			for _, l2Block := range lastValid.L2Blocks {
				if bytes.Equal(l2Block.ChainId, chainID) {
					headBlock = l2Block
					break
				}
			}
		}

		if headBlock == nil {
			if latest, err := c.l2BlockStore.GetLatestL2Block(ctx, chainID); err == nil && latest != nil {
				headBlock = latest
			}
		}

		request := &pb.L2BlockRequest{
			ChainId: append([]byte(nil), chainID...),
		}

		if headBlock != nil {
			request.BlockNumber = headBlock.BlockNumber + 1
			request.ParentHash = append([]byte(nil), headBlock.BlockHash...)
		} else {
			request.BlockNumber = 0
			request.ParentHash = nil
			c.log.Warn().
				Str("chain_id", fmt.Sprintf("%x", chainID)).
				Msg("No prior L2 block found during rollback; requesting genesis block")
		}

		requests = append(requests, request)
	}

	return requests, nil
}

func (c *Coordinator) sendRollBackAndStartSlot(
	ctx context.Context,
	currentSlot uint64,
	nextSuperblockNumber uint64,
	lastSuperblockHash []byte,
	requests []*pb.L2BlockRequest,
) error {
	msg := &pb.Message{
		SenderId: "shared-publisher",
		Payload: &pb.Message_RollBackAndStartSlot{
			RollBackAndStartSlot: &pb.RollBackAndStartSlot{
				L2BlocksRequest:      requests,
				CurrentSlot:          currentSlot,
				NextSuperblockNumber: nextSuperblockNumber,
				LastSuperblockHash:   append([]byte(nil), lastSuperblockHash...),
			},
		},
	}

	c.log.Info().
		Uint64("slot", currentSlot).
		Uint64("next_superblock_number", nextSuperblockNumber).
		Int("l2_requests", len(requests)).
		Msg("Broadcasting RollBackAndStartSlot message to sequencers")

	return c.transport.Broadcast(ctx, msg, "")
}

func (c *Coordinator) requeueRolledBackTransactions(ctx context.Context, slot uint64) error {
	var snapshot *SlotExecution

	c.executionHistoryMu.RLock()
	if exec, ok := c.executionHistory[slot]; ok {
		snapshot = exec
	}
	c.executionHistoryMu.RUnlock()

	if snapshot == nil && c.currentExecution != nil && c.currentExecution.Slot == slot {
		snapshot = c.currentExecution
	}

	if snapshot == nil || len(snapshot.AttemptedRequests) == 0 {
		c.log.Warn().
			Uint64("slot", slot).
			Msg("No attempted transactions recorded for rolled-back slot; callers must re-submit")
		return nil
	}

	requests := make([]*queue.QueuedXTRequest, 0, len(snapshot.AttemptedRequests))
	for _, req := range snapshot.AttemptedRequests {
		clone := *req
		if req.Request != nil {
			clone.Request = proto.Clone(req.Request).(*pb.XTRequest)
		}
		clone.XtID = append([]byte(nil), req.XtID...)
		requests = append(requests, &clone)
	}

	if err := c.xtQueue.RequeueForSlot(ctx, requests); err != nil {
		return err
	}

	// Clear attempted requests to avoid double requeue on subsequent recovery paths.
	c.executionHistoryMu.Lock()
	if entry, exists := c.executionHistory[snapshot.Slot]; exists {
		entry.AttemptedRequests = make(map[string]*queue.QueuedXTRequest)
	}
	c.executionHistoryMu.Unlock()

	c.mu.Lock()
	if c.currentExecution != nil && c.currentExecution.Slot == snapshot.Slot {
		c.currentExecution.AttemptedRequests = make(map[string]*queue.QueuedXTRequest)
	}
	c.mu.Unlock()

	return nil
}
