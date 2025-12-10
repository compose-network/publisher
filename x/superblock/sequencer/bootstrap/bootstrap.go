package bootstrap

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/compose-network/publisher/x/consensus"
	"github.com/compose-network/publisher/x/superblock/sequencer"
	"github.com/compose-network/publisher/x/transport"
	"github.com/compose-network/publisher/x/transport/tcp"
	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
)

// Config holds inputs to wire a sequencer with SBCP and P2P CIRC.
type Config struct {
	// ChainID is the ID of the chain the sequencer is running for.
	ChainID compose.ChainID
	// SPAddr is the address of the shared publisher in host:port format.
	SPAddr string
	// PeerAddrs is a map of chainID to host:port for other sequencers.
	// The chainID can be in decimal or hex format and will be normalized.
	PeerAddrs map[compose.ChainID]string

	// Log is the logger to use. If not provided, a no-op logger is used.
	Log zerolog.Logger

	// BaseConsensus is an optional existing 2PC coordinator. If nil, a default follower is created.
	BaseConsensus consensus.Coordinator

	// SPClientConfig is an optional override for the shared publisher client config.
	SPClientConfig *tcp.ClientConfig
	// P2PServerConfig is an optional override for the P2P server config.
	// If nil, tcp.DefaultServerConfig() is used.
	P2PServerConfig *transport.Config

	// P2PListenAddr is an optional P2P listen address, overriding P2PServerConfig.ListenAddr.
	P2PListenAddr string
}

// Runtime exposes the wired components and lifecycle.
type Runtime struct {
	// Coordinator is the sequencer coordinator.
	Coordinator sequencer.Coordinator
	// SPClient is the client for the shared publisher.
	SPClient transport.Client
	// P2PServer is the P2P server for CIRC.
	P2PServer transport.Server
	// Peers is a map of chainID key to peer client.
	Peers map[compose.ChainID]transport.Client

	log zerolog.Logger
	cfg Config
}

// Setup wires a sequencer coordinator, SP client, P2P server, and peer clients.
func Setup(ctx context.Context, cfg Config) (*Runtime, error) {
	log := cfg.Log
	if reflect.ValueOf(log).IsZero() {
		log = zerolog.Nop()
	}

	// Base consensus (2PC)
	base := cfg.BaseConsensus
	if base == nil {
		nodeID := fmt.Sprintf("sequencer-%d", time.Now().UnixNano())
		c := consensus.DefaultConfig(nodeID)
		c.Role = consensus.Follower
		c.IsLeader = false
		c.Timeout = time.Minute
		base = consensus.New(log, c)
	}

	// SP client
	spCfg := tcp.DefaultClientConfig()
	if cfg.SPClientConfig != nil {
		spCfg = *cfg.SPClientConfig
	}
	if cfg.SPAddr != "" {
		spCfg.ServerAddr = cfg.SPAddr
	}
	spClient := tcp.NewClient(spCfg, log)

	seqCfg := sequencer.Config{
		ChainID:              cfg.ChainID,
		BlockTimeout:         30 * time.Second,
		MaxLocalTxs:          1000,
		SCPTimeout:           10 * time.Second,
		EnableCIRCValidation: true,
	}
	coord := sequencer.NewSequencerCoordinator(base, seqCfg, spClient, log)

	// SP message handler routes to coordinator
	spClient.SetHandler(func(c context.Context, msg *pb.Message) ([]common.Hash, error) {
		return nil, coord.HandleMessage(c, msg.SenderId, msg)
	})

	// P2P server for CIRC
	var p2pSrv transport.Server
	if cfg.P2PServerConfig != nil {
		if cfg.P2PListenAddr != "" {
			cfg.P2PServerConfig.ListenAddr = cfg.P2PListenAddr
		}
		p2pSrv = tcp.NewServer(*cfg.P2PServerConfig, log)
	} else {
		s := tcp.DefaultServerConfig()
		if cfg.P2PListenAddr != "" {
			s.ListenAddr = cfg.P2PListenAddr
		}
		p2pSrv = tcp.NewServer(s, log)
	}
	p2pSrv.SetHandler(func(c context.Context, from string, msg *pb.Message) error {
		return coord.HandleMessage(c, from, msg)
	})

	log.Info().Interface("peer_addrs", cfg.PeerAddrs).Msg("Setting up peer clients")

	peers := make(map[compose.ChainID]transport.Client)
	for chainID, addr := range cfg.PeerAddrs {
		if strings.TrimSpace(addr) == "" {
			log.Warn().Uint64("chain_id", uint64(chainID)).Msg("Skipping peer with empty address")
			continue
		}
		log.Info().Uint64("chain_id", uint64(chainID)).Str("addr", addr).Msg("Creating peer client")
		cc := tcp.DefaultClientConfig()
		cc.ServerAddr = addr
		cc.ClientID = fmt.Sprintf("peer-%d", uint64(chainID))
		peers[chainID] = tcp.NewClient(cc, log)
	}

	log.Info().Int("peer_count", len(peers)).Interface("peer_keys", getPeerKeys(peers)).Msg("Peer clients created")

	rt := &Runtime{
		Coordinator: coord,
		SPClient:    spClient,
		P2PServer:   p2pSrv,
		Peers:       peers,
		log:         log,
		cfg:         cfg,
	}

	coord.SetCallbacks(sequencer.CoordinatorCallbacks{
		SendMailboxMessage: rt.SendMailboxMessage,
	})

	return rt, nil
}

// Start brings up coordinator, connects to SP, starts P2P, and dials peers.
func (r *Runtime) Start(ctx context.Context) error {
	if err := r.Coordinator.Start(ctx); err != nil {
		return fmt.Errorf("start coordinator: %w", err)
	}

	go func() {
		if err := r.P2PServer.Start(ctx); err != nil {
			r.log.Error().Err(err).Msg("P2P server failed")
		}
	}()

	if err := r.SPClient.ConnectWithRetry(ctx, "", 10); err != nil {
		return fmt.Errorf("connect SP: %w", err)
	}

	r.log.Info().
		Int("peer_count", len(r.Peers)).
		Interface("peer_keys", getPeerKeys(r.Peers)).
		Msg("Starting peer connections")

	for chainID, transportClient := range r.Peers {
		if addr, exists := r.cfg.PeerAddrs[chainID]; exists && strings.TrimSpace(addr) != "" {
			r.log.Info().
				Uint64("peer", uint64(chainID)).
				Str("addr", addr).
				Msg("Attempting to connect to peer")
			if err := transportClient.ConnectWithRetry(ctx, addr, 5); err != nil {
				r.log.Error().
					Uint64("peer", uint64(chainID)).
					Str("addr", addr).Err(err).
					Msg("Failed to connect to peer after retries")
			} else {
				r.log.Info().
					Uint64("peer", uint64(chainID)).
					Str("addr", addr).
					Msg("Successfully connected to peer")
			}
		} else {
			r.log.Error().
				Uint64("peer", uint64(chainID)).
				Interface("configured_addrs", r.cfg.PeerAddrs).
				Msg("No valid address configured for peer")
		}
	}
	return nil
}

// Stop stops coordinator and transports.
func (r *Runtime) Stop(ctx context.Context) error {
	r.log.Info().Msg("Stopping sequencer runtime")

	// Stop coordinator first to prevent new message processing
	_ = r.Coordinator.Stop(ctx)

	// Disconnect from shared publisher
	if err := r.SPClient.Disconnect(ctx); err != nil {
		r.log.Debug().Err(err).Msg("SP client disconnect error")
	}

	// Stop P2P server
	_ = r.P2PServer.Stop(ctx)

	// Disconnect all peer clients
	for key, c := range r.Peers {
		if err := c.Disconnect(ctx); err != nil {
			r.log.Debug().Uint64("peer", uint64(key)).Err(err).Msg("Peer disconnect error")
		}
	}

	r.log.Info().Msg("Sequencer runtime stopped")
	return nil
}

// SendMailboxMessage sends a Mailbox message to the peer indicated by DestinationChain.
func (r *Runtime) SendMailboxMessage(ctx context.Context, mailboxMsg *pb.MailboxMessage) error {
	r.log.Info().
		Uint64("dest_key", mailboxMsg.DestinationChain).
		Str("instance_id", string(mailboxMsg.InstanceId)).
		Msg("Sending Mailbox message to peer")

	peer, ok := r.Peers[compose.ChainID(mailboxMsg.DestinationChain)]
	if !ok || peer == nil {
		r.log.Error().
			Uint64("dest_key", mailboxMsg.DestinationChain).
			Str("instance_id", string(mailboxMsg.InstanceId)).
			Interface("available_peers", getPeerKeys(r.Peers)).
			Msg("No peer client found for destination chain")

		return fmt.Errorf("no peer for destination chain %d", mailboxMsg.DestinationChain)
	}

	msg := &pb.Message{Payload: &pb.Message_MailboxMessage{MailboxMessage: mailboxMsg}}

	if err := peer.Send(ctx, msg); err != nil {
		r.log.Error().
			Err(err).
			Uint64("dest_key", mailboxMsg.DestinationChain).
			Str("instance_id", string(mailboxMsg.InstanceId)).
			Msg("Failed to send Mailbox message to peer")
		return err
	}

	r.log.Info().
		Uint64("dest_key", mailboxMsg.DestinationChain).
		Str("instance_id", string(mailboxMsg.InstanceId)).
		Msg("Mailbox message sent successfully")

	return nil
}

// getPeerKeys returns a slice of available peer keys for logging
func getPeerKeys(peers map[compose.ChainID]transport.Client) []uint64 {
	keys := make([]uint64, 0, len(peers))
	for key := range peers {
		keys = append(keys, uint64(key))
	}
	return keys
}
