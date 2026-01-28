// Package main is the entry point for the compose-sidecar service.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/compose-network/compose-sdk/mailbox"
	"github.com/compose-network/compose-sdk/peer"
	"github.com/compose-network/compose-sdk/protocol"
	"github.com/compose-network/compose-sdk/simulation"
	"github.com/compose-network/compose-sdk/transport/quic"
	"github.com/compose-network/compose-sidecar/internal/config"
	"github.com/compose-network/compose-sidecar/internal/coordinator"
	"github.com/compose-network/compose-sidecar/internal/server"
	"github.com/compose-network/specs/compose/proto"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
	goproto "google.golang.org/protobuf/proto"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// run initializes and starts the compose-sidecar service,
// including HTTP and QUIC servers, simulators, and coordinators.
func run() error {
	// Parse flags
	configPath := flag.String("config", "", "path to config file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Setup logger
	log := setupLogger(cfg.Log)
	log.Info().Msg("Starting compose-sidecar")

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Extract chain IDs
	chainIDs := cfg.ChainIDs()

	// Create simulator with SDK simulation package
	simCfg := simulation.Config{
		Chains:  make([]simulation.ChainRPC, 0, len(cfg.Chains.Chains)),
		Timeout: cfg.Server.ReadTimeout,
	}
	for _, c := range cfg.Chains.Chains {
		simCfg.Chains = append(simCfg.Chains, simulation.ChainRPC{
			ChainID:        c.ID,
			URL:            c.RPC,
			MailboxAddress: common.HexToAddress(c.MailboxAddress),
		})
	}
	simulator, err := simulation.NewSimulator(simCfg)
	if err != nil {
		return fmt.Errorf("create simulator: %w", err)
	}

	// Use the first chain ID for the publisher client
	var primaryChainID uint64
	if len(chainIDs) > 0 {
		primaryChainID = chainIDs[0]
	}

	// Create QUIC client for publisher connection (if enabled)
	var pubClient quic.Client
	if cfg.Publisher.Enabled && cfg.Publisher.Addr != "" {
		pubCfg := quic.DefaultClientConfig()
		pubCfg.ServerAddr = stripProtocolPrefix(cfg.Publisher.Addr)
		pubCfg.ClientID = fmt.Sprintf("%d", primaryChainID)
		pubCfg.ReconnectDelay = cfg.Publisher.ReconnectDelay
		pubCfg.MaxRetries = cfg.Publisher.MaxRetries
		pubCfg.IdleTimeout = maxDuration(cfg.Server.ReadTimeout, pubCfg.IdleTimeout)
		pubCfg.MaxMessageSize = 10 * 1024 * 1024 // 10MB
		pubClient = quic.NewClient(pubCfg, log)
	}

	// Create QUIC clients for peer sidecars
	peerClients := make(map[uint64]quic.Client)
	for _, peer := range cfg.Peers.Sidecars {
		peerCfg := quic.DefaultClientConfig()
		peerCfg.ServerAddr = stripProtocolPrefix(peer.Addr)
		peerCfg.ClientID = fmt.Sprintf("%d", primaryChainID)
		peerCfg.ReconnectDelay = cfg.Publisher.ReconnectDelay
		peerCfg.MaxRetries = 3
		peerCfg.IdleTimeout = maxDuration(cfg.Server.ReadTimeout, peerCfg.IdleTimeout)
		peerCfg.MaxMessageSize = 10 * 1024 * 1024
		peerClients[peer.ChainID] = quic.NewClient(peerCfg, log)
	}

	// Create CIRC components using QUIC
	mailboxQueue := mailbox.NewMemoryQueue()
	peerCoordinatorPeers := make(map[uint64]peer.Client, len(peerClients))
	for chainID, client := range peerClients {
		peerCoordinatorPeers[chainID] = client
	}
	peerCoordinator := peer.NewCoordinator(primaryChainID, peerCoordinatorPeers, log)

	// Create mailbox sender wrapper for coordinator interface compatibility
	mailboxSender := &quicMailboxSender{
		peers: peerClients,
		log:   log,
	}

	// Create PutInboxBuilder if coordinator key is configured
	var putInboxBuilder coordinator.PutInboxBuilder
	if primaryChain, ok := cfg.GetChainByID(primaryChainID); ok && primaryChain.CoordinatorKey != "" {
		builder, err := coordinator.NewPutInboxBuilder(coordinator.PutInboxBuilderConfig{
			ChainID:        primaryChainID,
			MailboxAddress: common.HexToAddress(primaryChain.MailboxAddress),
			PrivateKeyHex:  primaryChain.CoordinatorKey,
			RPCURL:         primaryChain.RPC,
			Log:            log,
		})
		if err != nil {
			log.Warn().Err(err).Msg("Failed to create PutInboxBuilder - putInbox transactions will not be built")
		} else {
			putInboxBuilder = builder
			log.Info().Msg("PutInboxBuilder created successfully")
		}
	}

	// Create coordinator
	coord := coordinator.NewCoordinator(coordinator.CoordinatorConfig{
		ChainID:         primaryChainID,
		Simulator:       &simulatorAdapter{sim: simulator},
		Publisher:       &publisherAdapter{client: pubClient, chainID: primaryChainID},
		MailboxSender:   mailboxSender,
		MailboxQueue:    mailboxQueue,
		PeerCoordinator: peerCoordinator,
		PutInboxBuilder: putInboxBuilder,
		Log:             log,
	})

	// Set up publisher callbacks for v2 protocol
	if pubClient != nil {
		pubClient.SetHandler(func(ctx context.Context, clientID string, data []byte) error {
			return handlePublisherMessage(ctx, data, coord, log)
		})
	}

	// Set up peer message handlers
	for chainID, client := range peerClients {
		peerChainID := chainID
		client.SetHandler(func(ctx context.Context, clientID string, data []byte) error {
			return handlePeerMessage(ctx, data, coord, peerChainID, log)
		})
	}

	// Create HTTP server for builder polling
	srvCfg := server.Config{
		ListenAddr:   cfg.Server.ListenAddr,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}
	srv := server.NewServer(srvCfg, coord, log)

	// Create QUIC server for peer sidecar connections
	// QUIC uses UDP while HTTP uses TCP, so they can share the same port
	quicSrvCfg := quic.DefaultServerConfig()
	quicSrvCfg.ListenAddr = cfg.Server.ListenAddr
	quicSrvCfg.IdleTimeout = maxDuration(cfg.Server.ReadTimeout, quicSrvCfg.IdleTimeout)
	quicSrvCfg.MaxMessageSize = 10 * 1024 * 1024
	quicSrvCfg.MaxIncomingStreams = 100
	quicSrv := quic.NewServer(quicSrvCfg, log)

	// Set QUIC server handler for incoming peer messages
	quicSrv.SetHandler(func(ctx context.Context, clientID string, data []byte) error {
		return handlePeerMessage(ctx, data, coord, 0, log)
	})

	// Start components
	if err := coord.Start(ctx); err != nil {
		return fmt.Errorf("start coordinator: %w", err)
	}

	if err := srv.Start(ctx); err != nil {
		return fmt.Errorf("start server: %w", err)
	}

	if err := quicSrv.Start(ctx); err != nil {
		return fmt.Errorf("start QUIC server: %w", err)
	}

	// Connect to publisher if enabled (v1 mode with publisher coordination)
	if pubClient != nil {
		go func() {
			if err := pubClient.ConnectWithRetry(ctx); err != nil {
				log.Warn().Err(err).Msg("Failed to connect to publisher - running in standalone mode")
			}
		}()
	} else {
		log.Info().Msg("Running in standalone mode (v2) - no publisher connection")
	}

	// Connect to peer sidecars
	for chainID, client := range peerClients {
		peerChainID := chainID
		go func(c quic.Client) {
			if err := c.ConnectWithRetry(ctx); err != nil {
				log.Warn().Err(err).Uint64("peer_chain", peerChainID).Msg("Failed to connect to peer sidecar")
			}
		}(client)
	}

	log.Info().
		Str("http_addr", cfg.Server.ListenAddr).
		Str("quic_addr", cfg.Server.ListenAddr).
		Bool("publisher_enabled", cfg.Publisher.Enabled).
		Str("publisher_addr", cfg.Publisher.Addr).
		Uint64("chain_id", primaryChainID).
		Int("chains", len(cfg.Chains.Chains)).
		Int("peers", len(peerClients)).
		Msg("Sidecar started")

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Info().Msg("Shutting down")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Server.WriteTimeout)
	defer shutdownCancel()

	if err := srv.Stop(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Server shutdown error")
	}

	if err := quicSrv.Stop(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("QUIC server shutdown error")
	}

	if pubClient != nil {
		if err := pubClient.Disconnect(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("Publisher disconnect error")
		}
	}

	for chainID, client := range peerClients {
		if err := client.Disconnect(shutdownCtx); err != nil {
			log.Error().Err(err).Uint64("peer_chain", chainID).Msg("Peer disconnect error")
		}
	}

	if err := coord.Stop(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Coordinator shutdown error")
	}

	log.Info().Msg("Shutdown complete")
	return nil
}

func setupLogger(cfg config.LogConfig) zerolog.Logger {
	var log zerolog.Logger

	if cfg.Format == "pretty" {
		log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
			With().
			Timestamp().
			Logger()
	} else {
		log = zerolog.New(os.Stderr).
			With().
			Timestamp().
			Logger()
	}

	switch cfg.Level {
	case "debug":
		log = log.Level(zerolog.DebugLevel)
	case "warn":
		log = log.Level(zerolog.WarnLevel)
	case "error":
		log = log.Level(zerolog.ErrorLevel)
	default:
		log = log.Level(zerolog.InfoLevel)
	}

	return log
}

// handlePublisherMessage handles messages from the publisher.
func handlePublisherMessage(ctx context.Context, data []byte, coord *coordinator.DefaultCoordinator, log zerolog.Logger) error {
	var msg proto.Message
	if err := goproto.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("unmarshal publisher message: %w", err)
	}

	switch payload := msg.Payload.(type) {
	case *proto.Message_StartInstance:
		return coord.HandleStartInstance(ctx, payload.StartInstance)
	case *proto.Message_Decided:
		return coord.OnDecision(ctx, payload.Decided.InstanceIDHex(), payload.Decided.Decision)
	case *proto.Message_StartPeriod:
		return coord.HandleStartPeriod(ctx, payload.StartPeriod.PeriodId, payload.StartPeriod.SuperblockNumber)
	case *proto.Message_MailboxMessage:
		return coord.HandleMailboxMessage(ctx, payload.MailboxMessage)
	default:
		log.Debug().Str("type", fmt.Sprintf("%T", payload)).Msg("Unhandled publisher message type")
		return nil
	}
}

// handlePeerMessage handles messages from peer sidecars.
func handlePeerMessage(ctx context.Context, data []byte, coord *coordinator.DefaultCoordinator, peerChainID uint64, log zerolog.Logger) error {
	// Try to decode as JSON messages (XTForwardRequest or VoteRequest)
	var xtForward peer.XTForwardRequest
	if err := json.Unmarshal(data, &xtForward); err == nil && xtForward.InstanceID != "" {
		// Parse hex transactions back to bytes
		txs := make(map[uint64][]byte)
		for chainIDStr, hexTx := range xtForward.Transactions {
			var chainID uint64
			fmt.Sscanf(chainIDStr, "%d", &chainID)
			txBytes := common.FromHex(hexTx)
			txs[chainID] = txBytes
		}
		return coord.HandleForwardedXT(ctx, xtForward.InstanceID, txs, xtForward.LeaderChain)
	}

	var voteReq peer.VoteRequest
	if err := json.Unmarshal(data, &voteReq); err == nil && voteReq.InstanceID != "" {
		return coord.HandlePeerVote(ctx, voteReq.InstanceID, voteReq.ChainID, voteReq.Vote)
	}

	// Try as mailbox message (protobuf)
	var mailboxMsg proto.MailboxMessage
	if err := goproto.Unmarshal(data, &mailboxMsg); err == nil {
		return coord.HandleMailboxMessage(ctx, &mailboxMsg)
	}

	log.Debug().Msg("Unrecognized peer message format")
	return nil
}

func stripProtocolPrefix(addr string) string {
	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.TrimPrefix(addr, "https://")
	return addr
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// Adapter types to bridge SDK components with coordinator interfaces

type simulatorAdapter struct {
	sim simulation.Simulator
}

func (a *simulatorAdapter) Simulate(ctx context.Context, chainID uint64, tx []byte, stateOverrides map[string]interface{}) (*protocol.SimulationResult, error) {
	result, err := a.sim.Simulate(ctx, chainID, tx, stateOverrides)
	if err != nil {
		return nil, err
	}
	return convertSimulationResult(result), nil
}

func (a *simulatorAdapter) SimulateWithMailbox(
	ctx context.Context,
	chainID uint64,
	tx []byte,
	stateOverrides map[string]interface{},
	alreadySentMsgs []protocol.CrossRollupMessage,
	fulfilledDeps []protocol.CrossRollupDependency,
) (*protocol.SimulationResult, error) {
	// Convert coordinator types to SDK types
	sdkMsgs := convertToSDKMessages(alreadySentMsgs)
	sdkDeps := convertToSDKDeps(fulfilledDeps)

	result, err := a.sim.SimulateWithMailbox(ctx, chainID, tx, stateOverrides, sdkMsgs, sdkDeps)
	if err != nil {
		return nil, err
	}
	return convertSimulationResult(result), nil
}

type publisherAdapter struct {
	client  quic.Client
	chainID uint64
}

func (a *publisherAdapter) Connect(ctx context.Context) error {
	if a.client == nil {
		return fmt.Errorf("publisher client not configured")
	}
	return a.client.Connect(ctx)
}

func (a *publisherAdapter) ConnectWithRetry(ctx context.Context) error {
	if a.client == nil {
		return fmt.Errorf("publisher client not configured")
	}
	return a.client.ConnectWithRetry(ctx)
}

func (a *publisherAdapter) Disconnect(ctx context.Context) error {
	if a.client == nil {
		return nil
	}
	return a.client.Disconnect(ctx)
}

func (a *publisherAdapter) SendVote(ctx context.Context, instanceID []byte, vote bool) error {
	if a.client == nil {
		return fmt.Errorf("publisher client not configured")
	}

	voteMsg := &proto.Vote{
		InstanceId: instanceID,
		ChainId:    a.chainID,
		Vote:       vote,
	}

	msg := &proto.Message{
		Payload: &proto.Message_Vote{Vote: voteMsg},
	}

	data, err := goproto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal vote: %w", err)
	}

	return a.client.SendRaw(ctx, data)
}

func (a *publisherAdapter) SendRaw(ctx context.Context, data []byte) error {
	if a.client == nil {
		return fmt.Errorf("publisher client not configured")
	}
	return a.client.SendRaw(ctx, data)
}

func (a *publisherAdapter) IsConnected() bool {
	if a.client == nil {
		return false
	}
	return a.client.IsConnected()
}

func (a *publisherAdapter) SetOnStart(fn coordinator.StartCallback) {
	// Handled via message handler
}

func (a *publisherAdapter) SetOnDecision(fn coordinator.VoteCallback) {
	// Handled via message handler
}

type quicMailboxSender struct {
	peers map[uint64]quic.Client
	log   zerolog.Logger
}

func (s *quicMailboxSender) Send(ctx context.Context, destChainID uint64, msg *proto.MailboxMessage) error {
	client, ok := s.peers[destChainID]
	if !ok {
		return fmt.Errorf("no peer client for chain %d", destChainID)
	}

	if !client.IsConnected() {
		return fmt.Errorf("peer chain %d not connected", destChainID)
	}

	data, err := goproto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal mailbox message: %w", err)
	}

	return client.SendRaw(ctx, data)
}

// Type conversion helpers

func convertSimulationResult(result *simulation.Result) *protocol.SimulationResult {
	return &protocol.SimulationResult{
		ChainID:          result.ChainID,
		Success:          result.Success,
		Error:            result.Error,
		GasUsed:          result.GasUsed,
		Dependencies:     convertFromSDKDeps(result.Dependencies),
		OutboundMessages: convertFromSDKMessages(result.OutboundMessages),
	}
}

func convertToSDKMessages(msgs []protocol.CrossRollupMessage) []mailbox.CrossRollupMessage {
	result := make([]mailbox.CrossRollupMessage, len(msgs))
	for i, m := range msgs {
		result[i] = mailbox.CrossRollupMessage{
			SourceChainID: m.SourceChainID,
			DestChainID:   m.DestChainID,
			Sender:        m.Sender,
			Receiver:      m.Receiver,
			SessionID:     m.SessionID,
			Data:          m.Data,
			Label:         m.Label,
			MessageType:   m.MessageType,
			IsOutboxWrite: m.IsOutboxWrite,
		}
	}
	return result
}

func convertFromSDKMessages(msgs []mailbox.CrossRollupMessage) []protocol.CrossRollupMessage {
	result := make([]protocol.CrossRollupMessage, len(msgs))
	for i, m := range msgs {
		result[i] = protocol.CrossRollupMessage{
			SourceChainID: m.SourceChainID,
			DestChainID:   m.DestChainID,
			Sender:        m.Sender,
			Receiver:      m.Receiver,
			SessionID:     m.SessionID,
			Data:          m.Data,
			Label:         m.Label,
			MessageType:   m.MessageType,
			IsOutboxWrite: m.IsOutboxWrite,
		}
	}
	return result
}

func convertToSDKDeps(deps []protocol.CrossRollupDependency) []mailbox.CrossRollupDependency {
	result := make([]mailbox.CrossRollupDependency, len(deps))
	for i, d := range deps {
		result[i] = mailbox.CrossRollupDependency{
			SourceChainID: d.SourceChainID,
			DestChainID:   d.DestChainID,
			Sender:        d.Sender,
			Receiver:      d.Receiver,
			SessionID:     d.SessionID,
			Label:         d.Label,
			RequiredData:  d.RequiredData,
			IsInboxRead:   d.IsInboxRead,
			Data:          d.Data,
		}
	}
	return result
}

func convertFromSDKDeps(deps []mailbox.CrossRollupDependency) []protocol.CrossRollupDependency {
	result := make([]protocol.CrossRollupDependency, len(deps))
	for i, d := range deps {
		result[i] = protocol.CrossRollupDependency{
			SourceChainID: d.SourceChainID,
			DestChainID:   d.DestChainID,
			Sender:        d.Sender,
			Receiver:      d.Receiver,
			SessionID:     d.SessionID,
			Label:         d.Label,
			RequiredData:  d.RequiredData,
			IsInboxRead:   d.IsInboxRead,
			Data:          d.Data,
		}
	}
	return result
}
