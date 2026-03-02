package sidecar

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/compose-network/compose-sdk/consensus"
	"github.com/compose-network/compose-sdk/transport/quic"
	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/compose-network/specs/compose/sbcp"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

// txStartInfo holds pre-computed data for the consensus start callback.
type txStartInfo struct {
	periodID compose.PeriodID
	seqNum   compose.SequenceNumber
}

// pendingXTEntry is an XTRequest waiting for its chains to become free.
type pendingXTEntry struct {
	clientID string
	xtReq    *pb.XTRequest
	chains   []compose.ChainID
}

// maxPendingQueueSize is the upper bound on queued XTRequests to prevent
// unbounded memory growth during sustained burst load.
const maxPendingQueueSize = 100

// Coordinator manages sidecar connections and cross-chain transaction coordination
// using QUIC transport and 2PC consensus from compose-sdk.
type Coordinator struct {
	mu         sync.RWMutex
	quicServer quic.Server
	consensus  consensus.Coordinator
	log        zerolog.Logger

	// Track connected sidecars by chain ID
	chainToClient map[compose.ChainID]string // chainID → clientID
	clientToChain map[string]compose.ChainID // clientID → chainID

	// Track active transactions and their participants
	txParticipants map[string][]compose.ChainID // xtID → participating chainIDs
	txStartInfos   map[string]txStartInfo       // xtID → pre-computed start info
	activeChains   map[compose.ChainID]string   // chainID → active xtID

	// Queue of XTRequests that arrived while their chains were occupied.
	// Drained in FIFO order whenever an instance decides and frees its chains.
	pendingQueue []pendingXTEntry

	// Sequence numbering for StartInstance messages
	currentPeriodID compose.PeriodID
	nextSequenceNum compose.SequenceNumber

	// Statistics counters
	messagesProcessed atomic.Uint64
	broadcastsSent    atomic.Uint64
	startTime         time.Time
}

// NewCoordinator creates a new sidecar coordinator.
func NewCoordinator(server quic.Server, cons consensus.Coordinator, log zerolog.Logger) *Coordinator {
	return &Coordinator{
		quicServer:     server,
		consensus:      cons,
		log:            log.With().Str("component", "sidecar.coordinator").Logger(),
		chainToClient:  make(map[compose.ChainID]string),
		clientToChain:  make(map[string]compose.ChainID),
		txParticipants: make(map[string][]compose.ChainID),
		txStartInfos:   make(map[string]txStartInfo),
		activeChains:   make(map[compose.ChainID]string),
		pendingQueue:   make([]pendingXTEntry, 0),
		startTime:      time.Now(),
	}
}

// Start initializes the coordinator, wires up callbacks, and starts the QUIC server.
func (c *Coordinator) Start(ctx context.Context) error {
	c.consensus.SetStartCallback(c.onTransactionStart)
	c.consensus.SetDecisionCallback(c.onDecision)

	c.quicServer.SetHandler(c.handleMessage)
	c.quicServer.SetOnConnect(c.onConnect)

	if err := c.consensus.Start(ctx); err != nil {
		return err
	}

	if err := c.quicServer.Start(ctx); err != nil {
		c.consensus.Stop(ctx)
		return err
	}

	c.log.Info().Msg("Sidecar coordinator started")
	return nil
}

// Stop gracefully shuts down the coordinator.
func (c *Coordinator) Stop(ctx context.Context) error {
	c.log.Info().Msg("Stopping sidecar coordinator")

	if err := c.consensus.Stop(ctx); err != nil {
		c.log.Error().Err(err).Msg("Failed to stop consensus")
	}

	if err := c.quicServer.Stop(ctx); err != nil {
		c.log.Error().Err(err).Msg("Failed to stop QUIC server")
		return err
	}

	return nil
}

// GetStats returns statistics about the coordinator state.
func (c *Coordinator) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"active_connections":      c.quicServer.ConnectionCount(),
		"registered_chains":       len(c.chainToClient),
		"active_2pc_transactions": len(c.consensus.GetActiveTransactions()),
		"active_chains":           len(c.activeChains),
		"queued_xts":              len(c.pendingQueue),
		"messages_processed":      c.messagesProcessed.Load(),
		"broadcasts_sent":         c.broadcastsSent.Load(),
		"uptime_seconds":          time.Since(c.startTime).Seconds(),
		"chains_count":            len(c.chainToClient),
	}
}

// RegisterChain associates a client ID with a chain ID.
func (c *Coordinator) RegisterChain(clientID string, chainID compose.ChainID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if oldChain, ok := c.clientToChain[clientID]; ok {
		delete(c.chainToClient, oldChain)
	}

	c.chainToClient[chainID] = clientID
	c.clientToChain[clientID] = chainID

	c.log.Info().
		Str("client_id", clientID).
		Uint64("chain_id", uint64(chainID)).
		Msg("Chain registered")
}

// StartPeriod broadcasts a StartPeriod message to all connected sidecars.
func (c *Coordinator) StartPeriod(ctx context.Context, periodID compose.PeriodID, superblockNum compose.SuperblockNumber) error {
	c.mu.Lock()
	c.currentPeriodID = periodID
	c.nextSequenceNum = 1
	c.mu.Unlock()

	msg := &pb.Message{
		SenderId: "publisher",
		Payload: &pb.Message_StartPeriod{
			StartPeriod: &pb.StartPeriod{
				PeriodId:         uint64(periodID),
				SuperblockNumber: uint64(superblockNum),
			},
		},
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	c.log.Info().
		Uint64("period_id", uint64(periodID)).
		Uint64("superblock_num", uint64(superblockNum)).
		Msg("Broadcasting StartPeriod")

	c.broadcastsSent.Add(1)
	return c.quicServer.BroadcastRaw(ctx, data, "")
}

// HandleMailboxRelay forwards a MailboxMessage to the destination sidecar.
func (c *Coordinator) HandleMailboxRelay(ctx context.Context, mailbox *pb.MailboxMessage) error {
	c.mu.RLock()
	clientID, ok := c.chainToClient[compose.ChainID(mailbox.DestinationChain)]
	c.mu.RUnlock()

	if !ok {
		c.log.Warn().
			Uint64("dest_chain", uint64(mailbox.DestinationChain)).
			Msg("No sidecar registered for destination chain")
		return nil
	}

	msg := &pb.Message{
		SenderId: "publisher",
		Payload: &pb.Message_MailboxMessage{
			MailboxMessage: mailbox,
		},
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	c.broadcastsSent.Add(1)
	return c.quicServer.SendRaw(ctx, clientID, data)
}

// BroadcastRollback sends a Rollback message to all connected sidecars.
func (c *Coordinator) BroadcastRollback(ctx context.Context, periodID, lastSBNum uint64, lastSBHash []byte) error {
	msg := &pb.Message{
		SenderId: "publisher",
		Payload: &pb.Message_Rollback{
			Rollback: &pb.Rollback{
				PeriodId:                      periodID,
				LastFinalizedSuperblockNumber: lastSBNum,
				LastFinalizedSuperblockHash:   lastSBHash,
			},
		},
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	c.log.Info().
		Uint64("period_id", periodID).
		Uint64("last_sb_num", lastSBNum).
		Msg("Broadcasting Rollback")

	c.broadcastsSent.Add(1)
	return c.quicServer.BroadcastRaw(ctx, data, "")
}

// onConnect is called when a new QUIC client connection is established.
func (c *Coordinator) onConnect(ctx context.Context, clientID string, conn *quic.Connection) {
	c.log.Info().
		Str("client_id", clientID).
		Msg("New sidecar connection established")
}

// onTransactionStart is called by consensus when a new transaction should be broadcast.
// The xtID is the hex-encoded instance ID that was pre-computed in handleXTRequest.
func (c *Coordinator) onTransactionStart(xtID string, chains []compose.ChainID, data []byte) error {
	c.mu.Lock()
	c.txParticipants[xtID] = chains
	info, ok := c.txStartInfos[xtID]
	delete(c.txStartInfos, xtID)
	c.mu.Unlock()

	if !ok {
		c.log.Error().Str("xt_id", xtID).Msg("Missing start info for transaction")
		return nil
	}

	var xtReq pb.XTRequest
	if err := proto.Unmarshal(data, &xtReq); err != nil {
		c.log.Error().Err(err).Str("xt_id", xtID).Msg("Failed to unmarshal XTRequest")
		return err
	}

	instanceIDBytes, err := hex.DecodeString(xtID)
	if err != nil {
		return err
	}

	msg := &pb.Message{
		SenderId: "publisher",
		Payload: &pb.Message_StartInstance{
			StartInstance: &pb.StartInstance{
				InstanceId:     instanceIDBytes,
				PeriodId:       uint64(info.periodID),
				SequenceNumber: uint64(info.seqNum),
				XtRequest:      &xtReq,
			},
		},
	}

	msgData, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	c.log.Info().
		Str("xt_id", xtID).
		Uint64("period_id", uint64(info.periodID)).
		Uint64("seq_num", uint64(info.seqNum)).
		Int("chains", len(chains)).
		Msg("Broadcasting StartInstance to sidecars")

	c.broadcastsSent.Add(1)
	return c.quicServer.BroadcastRaw(context.Background(), msgData, "")
}

// onDecision is called by consensus when a decision is made.
func (c *Coordinator) onDecision(xtID string, decision bool) error {
	instanceIDBytes, err := hex.DecodeString(xtID)
	if err != nil {
		return err
	}

	msg := &pb.Message{
		SenderId: "publisher",
		Payload: &pb.Message_Decided{
			Decided: &pb.Decided{
				InstanceId: instanceIDBytes,
				Decision:   decision,
			},
		},
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	c.log.Info().
		Str("xt_id", xtID).
		Bool("decision", decision).
		Msg("Broadcasting Decided to sidecars")

	c.broadcastsSent.Add(1)
	broadcastErr := c.quicServer.BroadcastRaw(context.Background(), data, "")

	// Release chains after the broadcast so no queued XT can be started
	// before all participants have received the decision.
	c.mu.Lock()
	c.releaseInstanceChainsLocked(xtID)
	c.mu.Unlock()

	// Start any queued XTs whose chains are now free.
	c.drainQueue()

	return broadcastErr
}

// handleMessage routes incoming messages from sidecars.
func (c *Coordinator) handleMessage(ctx context.Context, clientID string, data []byte) error {
	c.messagesProcessed.Add(1)

	var msg pb.Message
	if err := proto.Unmarshal(data, &msg); err != nil {
		c.log.Error().Err(err).Str("client", clientID).Msg("Failed to unmarshal message")
		return err
	}

	switch payload := msg.Payload.(type) {
	case *pb.Message_Vote:
		return c.handleVote(ctx, clientID, payload.Vote)
	case *pb.Message_XtRequest:
		return c.handleXTRequest(ctx, clientID, payload.XtRequest)
	case *pb.Message_Ping:
		return c.handlePing(ctx, clientID, payload.Ping)
	case *pb.Message_HandshakeRequest:
		return c.handleHandshake(ctx, clientID, payload.HandshakeRequest)
	case *pb.Message_MailboxMessage:
		return c.HandleMailboxRelay(ctx, payload.MailboxMessage)
	default:
		c.log.Warn().Str("client", clientID).Type("type", payload).Msg("Unknown message type")
		return nil
	}
}

// handleVote processes a Vote message from a sidecar.
func (c *Coordinator) handleVote(ctx context.Context, clientID string, vote *pb.Vote) error {
	xtID := hex.EncodeToString(vote.InstanceId)

	c.log.Info().
		Str("xt_id", xtID).
		Uint64("chain_id", vote.ChainId).
		Bool("vote", vote.Vote).
		Str("client_id", clientID).
		Msg("Received vote from sidecar")

	_, _, err := c.consensus.RecordVote(xtID, compose.ChainID(vote.ChainId), vote.Vote)
	return err
}

// handleXTRequest processes an XTRequest message from a sidecar.
// If the requested chains are currently occupied by an active instance, the
// request is queued and started once those chains become free.
func (c *Coordinator) handleXTRequest(ctx context.Context, clientID string, xtReq *pb.XTRequest) error {
	var chains []compose.ChainID
	for _, txReq := range xtReq.TransactionRequests {
		chains = append(chains, compose.ChainID(txReq.ChainId))
	}
	chains = dedupeChains(chains)

	c.mu.Lock()
	if _, _, blocked := c.findOverlapLocked(chains); blocked {
		if len(c.pendingQueue) >= maxPendingQueueSize {
			c.mu.Unlock()
			c.log.Warn().
				Str("client_id", clientID).
				Int("queue_size", maxPendingQueueSize).
				Msg("XTRequest queue full, rejecting")
			return fmt.Errorf("XTRequest queue full: too many pending requests")
		}
		c.pendingQueue = append(c.pendingQueue, pendingXTEntry{
			clientID: clientID,
			xtReq:    xtReq,
			chains:   chains,
		})
		queueSize := len(c.pendingQueue)
		c.mu.Unlock()
		c.log.Info().
			Str("client_id", clientID).
			Int("chains", len(chains)).
			Int("queue_size", queueSize).
			Msg("XTRequest queued, chains occupied by active instance")
		return nil
	}

	// startXT releases c.mu before calling consensus.StartTransaction.
	return c.startXT(ctx, clientID, xtReq, chains)
}

// handlePing processes a Ping message from a sidecar.
func (c *Coordinator) handlePing(ctx context.Context, clientID string, ping *pb.Ping) error {
	pong := &pb.Message{
		SenderId: "publisher",
		Payload: &pb.Message_Pong{
			Pong: &pb.Pong{
				Timestamp: ping.Timestamp,
			},
		},
	}

	data, err := proto.Marshal(pong)
	if err != nil {
		return err
	}

	return c.quicServer.SendRaw(ctx, clientID, data)
}

// handleHandshake processes a HandshakeRequest and registers the sidecar's chain.
func (c *Coordinator) handleHandshake(ctx context.Context, clientID string, req *pb.HandshakeRequest) error {
	c.log.Info().
		Str("client_id", clientID).
		Str("requested_id", req.ClientId).
		Msg("Received handshake request")

	// Register the chain using the client ID. The sidecar's client ID encodes its chain identity;
	// chain registration happens here so the coordinator can route messages by chain.
	if req.ClientId != "" {
		c.RegisterChain(clientID, parseChainID(req.ClientId))
	}

	resp := &pb.Message{
		SenderId: "publisher",
		Payload: &pb.Message_HandshakeResponse{
			HandshakeResponse: &pb.HandshakeResponse{
				Accepted:  true,
				SessionId: clientID,
			},
		},
	}

	data, err := proto.Marshal(resp)
	if err != nil {
		return err
	}

	return c.quicServer.SendRaw(ctx, clientID, data)
}

// removeClient cleans up chain mappings when a client disconnects.
func (c *Coordinator) removeClient(clientID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if chainID, ok := c.clientToChain[clientID]; ok {
		delete(c.chainToClient, chainID)
		delete(c.clientToChain, clientID)
		c.log.Info().
			Str("client_id", clientID).
			Uint64("chain_id", uint64(chainID)).
			Msg("Client disconnected, chain unregistered")
	}
}

// parseChainID extracts a chain ID from a client identifier string.
// Falls back to 0 if the string cannot be parsed.
func parseChainID(clientID string) compose.ChainID {
	var id uint64
	for _, ch := range clientID {
		if ch >= '0' && ch <= '9' {
			id = id*10 + uint64(ch-'0')
		} else {
			break
		}
	}
	return compose.ChainID(id)
}

// toComposeXTRequest converts a proto XTRequest to the spec compose.XTRequest.
func toComposeXTRequest(req *pb.XTRequest) compose.XTRequest {
	if req == nil {
		return compose.XTRequest{}
	}

	txs := make([]compose.TransactionRequest, 0, len(req.GetTransactionRequests()))
	for _, tr := range req.GetTransactionRequests() {
		if tr == nil {
			continue
		}
		txs = append(txs, compose.TransactionRequest{
			ChainID:      compose.ChainID(tr.ChainId),
			Transactions: compose.CloneByteSlices(tr.Transaction),
		})
	}
	return compose.XTRequest{Transactions: txs}
}

func dedupeChains(chains []compose.ChainID) []compose.ChainID {
	if len(chains) < 2 {
		return chains
	}
	seen := make(map[compose.ChainID]struct{}, len(chains))
	deduped := make([]compose.ChainID, 0, len(chains))
	for _, chainID := range chains {
		if _, ok := seen[chainID]; ok {
			continue
		}
		seen[chainID] = struct{}{}
		deduped = append(deduped, chainID)
	}
	return deduped
}

// startXT starts a cross-chain transaction. c.mu must be held on entry; the
// lock is released before calling consensus.StartTransaction so the callback
// can re-acquire it without deadlocking.
func (c *Coordinator) startXT(ctx context.Context, clientID string, xtReq *pb.XTRequest, chains []compose.ChainID) error {
	composeReq := toComposeXTRequest(xtReq)
	seqNum := c.nextSequenceNum
	periodID := c.currentPeriodID
	instanceID := sbcp.GenerateInstanceID(periodID, seqNum, composeReq)
	xtID := instanceID.String()
	c.nextSequenceNum++
	c.txStartInfos[xtID] = txStartInfo{periodID: periodID, seqNum: seqNum}
	c.reserveInstanceChainsLocked(xtID, chains)
	c.mu.Unlock()

	data, err := proto.Marshal(xtReq)
	if err != nil {
		c.mu.Lock()
		delete(c.txStartInfos, xtID)
		c.releaseInstanceChainsLocked(xtID)
		c.mu.Unlock()
		return err
	}

	c.log.Info().
		Str("xt_id", xtID).
		Int("chains", len(chains)).
		Str("client_id", clientID).
		Msg("Starting cross-chain transaction")

	if err := c.consensus.StartTransaction(ctx, xtID, chains, data); err != nil {
		c.mu.Lock()
		delete(c.txStartInfos, xtID)
		c.releaseInstanceChainsLocked(xtID)
		c.mu.Unlock()
		return err
	}
	return nil
}

// drainQueue iterates the pending queue and starts every entry whose chains
// are no longer occupied. Stops when no further progress can be made.
func (c *Coordinator) drainQueue() {
	for {
		c.mu.Lock()
		entry, idx := c.findUnblockedQueued()
		if idx < 0 {
			c.mu.Unlock()
			return
		}
		c.pendingQueue = append(c.pendingQueue[:idx], c.pendingQueue[idx+1:]...)
		// startXT releases c.mu before calling consensus.StartTransaction.
		if err := c.startXT(context.Background(), entry.clientID, entry.xtReq, entry.chains); err != nil {
			c.log.Error().Err(err).
				Str("client_id", entry.clientID).
				Msg("Failed to start queued XT")
		}
	}
}

// findUnblockedQueued returns the first pending queue entry whose chains are
// all free and its index. Returns -1 when no such entry exists.
// c.mu must be held.
func (c *Coordinator) findUnblockedQueued() (pendingXTEntry, int) {
	for i, e := range c.pendingQueue {
		if _, _, blocked := c.findOverlapLocked(e.chains); !blocked {
			return e, i
		}
	}
	return pendingXTEntry{}, -1
}

func (c *Coordinator) findOverlapLocked(chains []compose.ChainID) (compose.ChainID, string, bool) {
	for _, chainID := range chains {
		if xtID, ok := c.activeChains[chainID]; ok {
			return chainID, xtID, true
		}
	}
	return 0, "", false
}

func (c *Coordinator) reserveInstanceChainsLocked(xtID string, chains []compose.ChainID) {
	c.txParticipants[xtID] = append([]compose.ChainID(nil), chains...)
	for _, chainID := range chains {
		c.activeChains[chainID] = xtID
	}
}

func (c *Coordinator) releaseInstanceChainsLocked(xtID string) {
	chains, ok := c.txParticipants[xtID]
	if !ok {
		return
	}
	for _, chainID := range chains {
		if activeXTID, exists := c.activeChains[chainID]; exists && activeXTID == xtID {
			delete(c.activeChains, chainID)
		}
	}
	delete(c.txParticipants, xtID)
}
