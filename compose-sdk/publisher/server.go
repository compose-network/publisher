package publisher

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/compose-network/compose-sdk/transport/quic"
	"github.com/compose-network/specs/compose"
	"github.com/compose-network/specs/compose/proto"
	"github.com/compose-network/specs/compose/sbcp"
	"github.com/compose-network/specs/compose/scp"
	"github.com/rs/zerolog"
	goproto "google.golang.org/protobuf/proto"
)

// Server runs the shared publisher over QUIC using the Compose v2 spec.
type Server struct {
	cfg Config
	log zerolog.Logger

	quicServer quic.Server

	sbPublisher sbcp.Publisher
	scpNetwork  *scpNetwork
	messenger   *publisherMessenger

	mu sync.Mutex

	periodInitialized bool
	currentPeriodID   uint64
	currentSuperblock uint64

	queue     []queuedRequest
	instances map[string]*instanceState

	started time.Time

	workCh chan struct{}

	msgCount      atomic.Uint64
	broadcastsCnt atomic.Uint64
}

// New creates a new QUIC shared publisher server.
func New(cfg Config, log zerolog.Logger) (*Server, error) {
	if cfg.Quic.ListenAddr == "" {
		return nil, fmt.Errorf("quic listen address is required")
	}

	s := &Server{
		cfg:       cfg,
		log:       log.With().Str("component", "compose.publisher").Logger(),
		queue:     make([]queuedRequest, 0),
		instances: make(map[string]*instanceState),
		workCh:    make(chan struct{}, 1),
	}

	s.scpNetwork = &scpNetwork{server: s}
	s.messenger = &publisherMessenger{server: s}

	sbPub, err := sbcp.NewPublisher(
		noopProver{log: s.log},
		s.messenger,
		noopL1{log: s.log},
		0,
		0,
		0,
		compose.SuperblockHash{},
		compose.ProofWindow,
		s.log,
		map[compose.ChainID]struct{}{},
	)
	if err != nil {
		return nil, fmt.Errorf("init sbcp publisher: %w", err)
	}
	s.sbPublisher = sbPub

	quicSrv := quic.NewServer(cfg.Quic, s.log)
	quicSrv.SetHandler(s.handleRawMessage)
	quicSrv.SetOnConnect(s.handleConnect)
	s.quicServer = quicSrv

	return s, nil
}

// Start starts the publisher (QUIC server + period ticker).
func (s *Server) Start(ctx context.Context) error {
	s.started = time.Now()

	if err := s.quicServer.Start(ctx); err != nil {
		return fmt.Errorf("start quic server: %w", err)
	}

	// Start worker loop for queue processing.
	go s.queueWorker(ctx)

	// Start first period immediately.
	if err := s.startPeriod(); err != nil {
		s.log.Error().Err(err).Msg("Failed to start initial period")
	}

	// Periodic StartPeriod ticker (optional).
	if s.cfg.PeriodDuration > 0 {
		ticker := time.NewTicker(s.cfg.PeriodDuration)
		go func() {
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := s.startPeriod(); err != nil {
						s.log.Error().Err(err).Msg("Failed to start new period")
					}
				}
			}
		}()
	}

	s.log.Info().
		Str("addr", s.cfg.Quic.ListenAddr).
		Dur("period_duration", s.cfg.PeriodDuration).
		Dur("instance_timeout", s.cfg.InstanceTimeout).
		Msg("Compose publisher started")

	return nil
}

// Stop stops the publisher.
func (s *Server) Stop(ctx context.Context) error {
	if err := s.quicServer.Stop(ctx); err != nil {
		return err
	}
	return nil
}

// GetStats returns operational metrics for diagnostics.
func (s *Server) GetStats() map[string]interface{} {
	s.mu.Lock()
	queueLen := len(s.queue)
	active := len(s.instances)
	s.mu.Unlock()

	connections := s.quicServer.ConnectionCount()

	activeIDs := make([]string, 0, active)
	s.mu.Lock()
	for id := range s.instances {
		activeIDs = append(activeIDs, id)
	}
	s.mu.Unlock()

	return map[string]interface{}{
		"uptime_seconds":          time.Since(s.started).Seconds(),
		"active_connections":      connections,
		"messages_processed":      s.msgCount.Load(),
		"broadcasts_sent":         s.broadcastsCnt.Load(),
		"chains_count":            connections,
		"active_2pc_transactions": active,
		"active_2pc_ids":          activeIDs,
		"queue_length":            queueLen,
	}
}

func (s *Server) handleConnect(ctx context.Context, clientID string, _ *quic.Connection) {
	s.mu.Lock()
	periodReady := s.periodInitialized
	periodID := s.currentPeriodID
	superblock := s.currentSuperblock
	s.mu.Unlock()

	if !periodReady {
		return
	}

	if err := s.sendStartPeriod(ctx, clientID, periodID, superblock); err != nil {
		s.log.Warn().
			Err(err).
			Str("client_id", clientID).
			Msg("Failed to send StartPeriod to new connection")
	}
}

func (s *Server) handleRawMessage(ctx context.Context, clientID string, data []byte) error {
	var msg proto.Message
	if err := goproto.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("unmarshal message: %w", err)
	}

	s.msgCount.Add(1)

	switch payload := msg.Payload.(type) {
	case *proto.Message_XtRequest:
		return s.enqueueXT(payload.XtRequest)
	case *proto.Message_Vote:
		return s.handleVote(payload.Vote)
	case *proto.Message_Ping:
		return s.handlePing(ctx, clientID, payload.Ping)
	default:
		s.log.Debug().Str("type", fmt.Sprintf("%T", payload)).Msg("Unhandled message type")
		return nil
	}
}

func (s *Server) handlePing(ctx context.Context, clientID string, ping *proto.Ping) error {
	if ping == nil {
		return nil
	}

	msg := &proto.Message{
		SenderId: "publisher",
		Payload: &proto.Message_Pong{
			Pong: &proto.Pong{Timestamp: ping.Timestamp},
		},
	}

	data, err := goproto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal pong: %w", err)
	}

	if err := s.quicServer.SendRaw(ctx, clientID, data); err != nil {
		return fmt.Errorf("send pong: %w", err)
	}

	s.broadcastsCnt.Add(1)
	return nil
}

func (s *Server) enqueueXT(req *proto.XTRequest) error {
	if req == nil {
		return fmt.Errorf("nil xt request")
	}

	composeReq := toComposeXTRequest(req)
	if len(composeReq.Transactions) < 2 {
		return fmt.Errorf("xt request must include at least two chains")
	}

	s.mu.Lock()
	s.queue = append(s.queue, queuedRequest{
		protoReq:   req,
		composeReq: composeReq,
		receivedAt: time.Now(),
	})
	s.mu.Unlock()

	s.signalWork()
	return nil
}

func (s *Server) handleVote(vote *proto.Vote) error {
	if vote == nil {
		return fmt.Errorf("nil vote")
	}

	instanceID := vote.InstanceIDHex()
	if instanceID == "" {
		return fmt.Errorf("empty instance id")
	}

	s.mu.Lock()
	state := s.instances[instanceID]
	s.mu.Unlock()

	if state == nil {
		s.log.Warn().
			Str("instance_id", instanceID).
			Uint64("chain_id", vote.ChainId).
			Msg("Vote for unknown instance")
		return nil
	}

	if err := state.scp.ProcessVote(compose.ChainID(vote.ChainId), vote.Vote); err != nil {
		if !errors.Is(err, scp.ErrDuplicatedVote) && !errors.Is(err, scp.ErrSenderNotParticipant) {
			s.log.Warn().Err(err).Str("instance_id", instanceID).Msg("Failed to process vote")
		}
	}

	if state.scp.DecisionState() != compose.DecisionStatePending {
		s.finalizeInstance(instanceID)
	}

	return nil
}

func (s *Server) startPeriod() error {
	if err := s.sbPublisher.StartPeriod(); err != nil {
		return err
	}
	return nil
}

func (s *Server) queueWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.workCh:
			s.processQueue()
		}
	}
}

func (s *Server) processQueue() {
	s.mu.Lock()
	periodReady := s.periodInitialized
	s.mu.Unlock()
	if !periodReady {
		return
	}

	type startInfo struct {
		state *instanceState
	}
	toStart := make([]startInfo, 0)

	s.mu.Lock()
	if len(s.queue) == 0 {
		s.mu.Unlock()
		return
	}

	remaining := make([]queuedRequest, 0, len(s.queue))
	for _, req := range s.queue {
		instance, err := s.sbPublisher.StartInstance(req.composeReq)
		if err != nil {
			if errors.Is(err, sbcp.ErrCannotStartInstance) {
				remaining = append(remaining, req)
				continue
			}
			s.log.Warn().Err(err).Msg("Dropping XT request")
			continue
		}

		scpInst, err := scp.NewPublisherInstance(instance, s.scpNetwork, s.log)
		if err != nil {
			s.log.Warn().Err(err).Msg("Failed to create SCP instance")
			continue
		}

		state := &instanceState{
			instance: instance,
			scp:      scpInst,
			protoReq: req.protoReq,
		}

		idHex := hex.EncodeToString(instance.ID[:])
		s.instances[idHex] = state
		toStart = append(toStart, startInfo{state: state})
	}
	s.queue = remaining
	s.mu.Unlock()

	for _, info := range toStart {
		info.state.scp.Run()
		s.startInstanceTimeout(info.state)
	}
}

func (s *Server) startInstanceTimeout(state *instanceState) {
	if s.cfg.InstanceTimeout <= 0 {
		return
	}

	idHex := hex.EncodeToString(state.instance.ID[:])
	state.timeout = time.AfterFunc(s.cfg.InstanceTimeout, func() {
		s.mu.Lock()
		current := s.instances[idHex]
		s.mu.Unlock()
		if current == nil {
			return
		}

		if err := current.scp.Timeout(); err != nil {
			s.log.Warn().Err(err).Str("instance_id", idHex).Msg("Instance timeout error")
		}
		s.finalizeInstance(idHex)
	})
}

func (s *Server) finalizeInstance(idHex string) {
	s.mu.Lock()
	state := s.instances[idHex]
	if state == nil {
		s.mu.Unlock()
		return
	}
	delete(s.instances, idHex)
	if state.timeout != nil {
		state.timeout.Stop()
	}
	s.mu.Unlock()

	if err := s.sbPublisher.DecideInstance(state.instance); err != nil && !errors.Is(err, sbcp.ErrChainNotActive) {
		s.log.Warn().Err(err).Str("instance_id", idHex).Msg("Failed to decide instance")
	}

	s.signalWork()
}

func (s *Server) signalWork() {
	select {
	case s.workCh <- struct{}{}:
	default:
	}
}

func (s *Server) sendStartInstance(instance compose.Instance) {
	idHex := hex.EncodeToString(instance.ID[:])

	s.mu.Lock()
	state := s.instances[idHex]
	s.mu.Unlock()
	if state == nil {
		s.log.Warn().Str("instance_id", idHex).Msg("StartInstance for unknown instance")
		return
	}

	start := &proto.StartInstance{
		InstanceId:     instance.ID[:],
		PeriodId:       uint64(instance.PeriodID),
		SequenceNumber: uint64(instance.SequenceNumber),
		XtRequest:      state.protoReq,
	}

	msg := &proto.Message{
		SenderId: "publisher",
		Payload:  &proto.Message_StartInstance{StartInstance: start},
	}

	data, err := goproto.Marshal(msg)
	if err != nil {
		s.log.Warn().Err(err).Str("instance_id", idHex).Msg("Failed to marshal StartInstance")
		return
	}

	for _, chainID := range instance.Chains() {
		clientID := fmt.Sprintf("%d", chainID)
		if err := s.quicServer.SendRaw(context.Background(), clientID, data); err != nil {
			s.log.Warn().
				Err(err).
				Str("instance_id", idHex).
				Str("client_id", clientID).
				Msg("Failed to send StartInstance")
			continue
		}
		s.broadcastsCnt.Add(1)
	}
}

func (s *Server) sendDecided(instanceID compose.InstanceID, decided bool) {
	idHex := hex.EncodeToString(instanceID[:])

	s.mu.Lock()
	state := s.instances[idHex]
	s.mu.Unlock()
	if state == nil {
		s.log.Warn().Str("instance_id", idHex).Msg("Decided for unknown instance")
		return
	}

	decidedMsg := &proto.Decided{
		InstanceId: instanceID[:],
		Decision:   decided,
	}

	msg := &proto.Message{
		SenderId: "publisher",
		Payload:  &proto.Message_Decided{Decided: decidedMsg},
	}

	data, err := goproto.Marshal(msg)
	if err != nil {
		s.log.Warn().Err(err).Str("instance_id", idHex).Msg("Failed to marshal Decided")
		return
	}

	for _, chainID := range state.instance.Chains() {
		clientID := fmt.Sprintf("%d", chainID)
		if err := s.quicServer.SendRaw(context.Background(), clientID, data); err != nil {
			s.log.Warn().
				Err(err).
				Str("instance_id", idHex).
				Str("client_id", clientID).
				Msg("Failed to send Decided")
			continue
		}
		s.broadcastsCnt.Add(1)
	}
}

func (s *Server) sendStartPeriod(ctx context.Context, clientID string, periodID, superblock uint64) error {
	msg := &proto.Message{
		SenderId: "publisher",
		Payload: &proto.Message_StartPeriod{
			StartPeriod: &proto.StartPeriod{
				PeriodId:         periodID,
				SuperblockNumber: superblock,
			},
		},
	}

	data, err := goproto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal start period: %w", err)
	}

	if err := s.quicServer.SendRaw(ctx, clientID, data); err != nil {
		return fmt.Errorf("send start period: %w", err)
	}

	s.broadcastsCnt.Add(1)
	return nil
}

type queuedRequest struct {
	protoReq   *proto.XTRequest
	composeReq compose.XTRequest
	receivedAt time.Time
}

type instanceState struct {
	instance compose.Instance
	scp      scp.PublisherInstance
	protoReq *proto.XTRequest
	timeout  *time.Timer
}

type scpNetwork struct {
	server *Server
}

func (n *scpNetwork) SendStartInstance(instance compose.Instance) {
	n.server.sendStartInstance(instance)
}

func (n *scpNetwork) SendDecided(instanceID compose.InstanceID, decided bool) {
	n.server.sendDecided(instanceID, decided)
}

type publisherMessenger struct {
	server *Server
}

func (m *publisherMessenger) BroadcastStartPeriod(periodID compose.PeriodID, superblock compose.SuperblockNumber) {
	m.server.mu.Lock()
	m.server.currentPeriodID = uint64(periodID)
	m.server.currentSuperblock = uint64(superblock)
	m.server.periodInitialized = true
	m.server.mu.Unlock()

	msg := &proto.Message{
		SenderId: "publisher",
		Payload: &proto.Message_StartPeriod{
			StartPeriod: &proto.StartPeriod{
				PeriodId:         uint64(periodID),
				SuperblockNumber: uint64(superblock),
			},
		},
	}

	data, err := goproto.Marshal(msg)
	if err != nil {
		m.server.log.Warn().Err(err).Msg("Failed to marshal StartPeriod")
		return
	}

	if err := m.server.quicServer.BroadcastRaw(context.Background(), data, ""); err != nil {
		m.server.log.Warn().Err(err).Msg("Failed to broadcast StartPeriod")
	}
	m.server.broadcastsCnt.Add(1)
	m.server.signalWork()
}

func (m *publisherMessenger) BroadcastRollback(periodID compose.PeriodID, superblock compose.SuperblockNumber, superblockHash compose.SuperblockHash) {
	msg := &proto.Message{
		SenderId: "publisher",
		Payload: &proto.Message_Rollback{
			Rollback: &proto.Rollback{
				PeriodId:                      uint64(periodID),
				LastFinalizedSuperblockNumber: uint64(superblock),
				LastFinalizedSuperblockHash:   superblockHash[:],
			},
		},
	}

	data, err := goproto.Marshal(msg)
	if err != nil {
		m.server.log.Warn().Err(err).Msg("Failed to marshal Rollback")
		return
	}

	if err := m.server.quicServer.BroadcastRaw(context.Background(), data, ""); err != nil {
		m.server.log.Warn().Err(err).Msg("Failed to broadcast Rollback")
	}
	m.server.broadcastsCnt.Add(1)
}

type noopProver struct {
	log zerolog.Logger
}

func (n noopProver) RequestSuperblockProof(_ compose.SuperblockNumber, _ compose.SuperblockHash, _ [][]byte) ([]byte, error) {
	return nil, fmt.Errorf("prover not configured")
}

type noopL1 struct {
	log zerolog.Logger
}

func (n noopL1) PublishProof(_ compose.SuperblockNumber, _ []byte) {}

func toComposeXTRequest(req *proto.XTRequest) compose.XTRequest {
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
