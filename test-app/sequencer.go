package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
	pb "github.com/ssvlabs/rollup-shared-publisher/proto/rollup/v1"
	"github.com/ssvlabs/rollup-shared-publisher/x/auth"
	"github.com/ssvlabs/rollup-shared-publisher/x/consensus"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/sequencer"
	"github.com/ssvlabs/rollup-shared-publisher/x/transport"
	"github.com/ssvlabs/rollup-shared-publisher/x/transport/tcp"
)

type Sequencer struct {
	name        string
	chainID     []byte
	coordinator sequencer.Coordinator // Now using the SBCP coordinator
	spClient    transport.Client
	spAddr      string
	log         zerolog.Logger
	authMgr     auth.Manager

	// P2P for CIRC
	p2pServer transport.Server
	p2pPort   string
	peers     map[string]transport.Client

	// Channels for test coordination
	decidedCh chan *pb.Decided
	circCh    chan *pb.CIRCMessage

	// State
	mu    sync.RWMutex
	voted map[string]bool
	xtID  *pb.XtID
}

func NewSequencer(name string, chainID []byte, spAddr string, log zerolog.Logger, authMgr auth.Manager) *Sequencer {
	// Determine P2P port based on chain ID
	chainIDStr := hex.EncodeToString(chainID)
	p2pPort := ":9000"
	if chainIDStr == "01046a" { // chain ID 66666 (second sequencer)
		p2pPort = ":9001"
	}

	s := &Sequencer{
		name:      name,
		chainID:   chainID,
		spAddr:    spAddr,
		log:       log.With().Str("seq", name).Logger(),
		authMgr:   authMgr,
		p2pPort:   p2pPort,
		peers:     make(map[string]transport.Client),
		decidedCh: make(chan *pb.Decided, 1),
		circCh:    make(chan *pb.CIRCMessage, 10),
		voted:     make(map[string]bool),
	}

	// Create SP client with auth
	spCfg := tcp.DefaultClientConfig()
	spCfg.ServerAddr = spAddr
	spCfg.ClientID = name

	spClient := tcp.NewClient(spCfg, s.log)
	if authMgr != nil {
		spClient = spClient.WithAuth(authMgr)
		s.log.Info().Str("auth", "enabled").Msg("SP client auth configured")
	}
	s.spClient = spClient

	// Create basic consensus coordinator
	consensusConfig := consensus.Config{
		NodeID:   name,
		IsLeader: false,
		Timeout:  5 * time.Second, // Shorter timeout to avoid long waits
		Role:     consensus.Follower,
	}
	baseConsensus := consensus.New(s.log, consensusConfig)

	// Wrap with SBCP sequencer coordinator
	seqConfig := sequencer.DefaultConfig(chainID)

	coord, err := sequencer.WrapCoordinator(baseConsensus, seqConfig, spClient, s.log)
	if err != nil {
		s.log.Fatal().Err(err).Msg("Failed to create sequencer coordinator")
	}
	s.coordinator = coord

	// Set SP client handler to route to coordinator
	spClient.SetHandler(s.handleSPMessage)

	// Create P2P server WITHOUT auth (simpler for CIRC)
	p2pCfg := tcp.DefaultServerConfig()
	p2pCfg.ListenAddr = p2pPort
	s.p2pServer = tcp.NewServer(p2pCfg, s.log)
	s.p2pServer.SetHandler(s.handleP2PMessage)

	return s
}

func (s *Sequencer) Start(ctx context.Context) error {
	// Start the coordinator
	if err := s.coordinator.Start(ctx); err != nil {
		return fmt.Errorf("failed to start coordinator: %w", err)
	}

	// Connect to SP
	if err := s.spClient.Connect(ctx, s.spAddr); err != nil {
		return fmt.Errorf("connect to SP: %w", err)
	}
	s.log.Info().Str("sp", s.spAddr).Msg("Connected to Shared Publisher")

	// Start P2P server
	go func() {
		if err := s.p2pServer.Start(ctx); err != nil {
			s.log.Error().Err(err).Msg("P2P server failed")
		}
	}()
	s.log.Info().Str("addr", s.p2pPort).Msg("P2P server started for CIRC")

	// Only chain 1 connects to chain 2 (avoid bidirectional)
	chainIDStr := hex.EncodeToString(s.chainID)
	if chainIDStr == "d903" { // chain ID 55555 (first sequencer)
		go func() {
			time.Sleep(1 * time.Second) // Wait for chain 2's server to start
			s.connectToPeer(ctx, "01046a", "localhost:9001")
		}()
	}

	return nil
}

func (s *Sequencer) connectToPeer(ctx context.Context, chainID string, addr string) {
	cfg := tcp.DefaultClientConfig()
	cfg.ServerAddr = addr
	cfg.ClientID = fmt.Sprintf("%s-p2p", s.name)
	cfg.ConnectTimeout = 5 * time.Second

	// NO AUTH for P2P connections (simpler)
	client := tcp.NewClient(cfg, s.log)
	client.SetHandler(s.handleP2PClientMessage)

	if err := client.Connect(ctx, addr); err != nil {
		s.log.Warn().
			Str("chain", chainID).
			Str("addr", addr).
			Err(err).
			Msg("Failed to connect to peer")
		return
	}

	s.mu.Lock()
	s.peers[chainID] = client
	s.mu.Unlock()

	s.log.Info().
		Str("chain", chainID).
		Str("addr", addr).
		Msg("Connected to peer sequencer")
}

// handleSPMessage routes messages from SP to coordinator
func (s *Sequencer) handleSPMessage(ctx context.Context, msg *pb.Message) ([]common.Hash, error) {
	s.log.Debug().
		Str("type", fmt.Sprintf("%T", msg.Payload)).
		Str("sender_id", msg.SenderId).
		Msg("Message from SP")

	// Handle specific messages for test coordination and CIRC/2PC
	switch p := msg.Payload.(type) {
	case *pb.Message_XtRequest:
		// Handle cross-chain transaction requests
		if err := s.handleXTRequest(ctx, p.XtRequest); err != nil {
			s.log.Error().Err(err).Msg("Failed to handle XTRequest")
			return nil, err
		}

	case *pb.Message_Decided:
		if p.Decided.Decision {
			s.log.Info().
				Str("xt_id", p.Decided.XtId.Hex()).
				Msg("✅ Transaction committed")
		} else {
			s.log.Warn().
				Str("xt_id", p.Decided.XtId.Hex()).
				Msg("❌ Transaction aborted")
		}
		select {
		case s.decidedCh <- p.Decided:
		default:
		}
		// Don't route Decided messages to SBCP coordinator - they're handled by original 2PC

	case *pb.Message_CircMessage:
		// Handle CIRC messages
		circ := p.CircMessage
		s.log.Info().
			Str("xt_id", circ.XtId.Hex()).
			Str("source", fmt.Sprintf("%x", circ.SourceChain)).
			Msg("Received CIRC from SP")
		select {
		case s.circCh <- circ:
		default:
		}
		// Route CIRC to coordinator
		if err := s.coordinator.HandleMessage(ctx, msg.SenderId, msg); err != nil {
			s.log.Error().Err(err).Msg("Coordinator failed to handle CIRC message")
		}
		return nil, nil

	default:
		// Route other messages to coordinator for SBCP handling
		if err := s.coordinator.HandleMessage(ctx, msg.SenderId, msg); err != nil {
			s.log.Error().Err(err).Msg("Coordinator failed to handle message")
			return nil, err
		}
	}

	return nil, nil
}

// handleP2PClientMessage handles P2P messages from client connections
func (s *Sequencer) handleP2PClientMessage(ctx context.Context, msg *pb.Message) ([]common.Hash, error) {
	if p, ok := msg.Payload.(*pb.Message_CircMessage); ok {
		circ := p.CircMessage
		s.log.Info().
			Str("source_chain", fmt.Sprintf("%x", circ.SourceChain)).
			Str("xt_id", circ.XtId.Hex()).
			Msg("📨 Received CIRC via client connection")

		// Forward to SP for consensus coordination
		s.forwardCIRCToSP(ctx, circ)

		// Route to coordinator for SBCP handling
		return nil, s.coordinator.HandleMessage(ctx, "p2p-client", msg)
	}
	return nil, nil
}

// handleP2PMessage handles messages from P2P server (incoming CIRC)
func (s *Sequencer) handleP2PMessage(ctx context.Context, from string, msg *pb.Message) error {
	switch p := msg.Payload.(type) {
	case *pb.Message_CircMessage:
		circ := p.CircMessage
		s.log.Info().
			Str("from", from).
			Str("source", fmt.Sprintf("%x", circ.SourceChain)).
			Str("dest", fmt.Sprintf("%x", circ.DestinationChain)).
			Str("xt_id", circ.XtId.Hex()).
			Msg("📨 Received CIRC from peer (server)")

		// Forward to SP for consensus coordination
		s.forwardCIRCToSP(ctx, circ)

		// Process locally
		select {
		case s.circCh <- circ:
		default:
		}

		// Route to coordinator
		return s.coordinator.HandleMessage(ctx, from, msg)

	default:
		s.log.Debug().
			Str("from", from).
			Str("type", fmt.Sprintf("%T", p)).
			Msg("Unhandled P2P message")
	}

	return nil
}

func (s *Sequencer) InitiateXT(ctx context.Context, xt *pb.XTRequest) error {
	xtID, _ := xt.XtID()

	msg := &pb.Message{
		SenderId: s.name,
		Payload:  &pb.Message_XtRequest{XtRequest: xt},
	}

	if err := s.spClient.Send(ctx, msg); err != nil {
		return err
	}

	s.log.Info().
		Str("xt_id", xtID.Hex()).
		Msg("XTRequest sent to SP")

	return nil
}

func (s *Sequencer) handleXTRequest(ctx context.Context, xtReq *pb.XTRequest) error {
	xtID, err := xtReq.XtID()
	if err != nil {
		return err
	}

	s.log.Info().
		Str("xt_id", xtID.Hex()).
		Int("txs", len(xtReq.Transactions)).
		Msg("Received XTRequest from SP")

	// Check if we participate
	chains := xtReq.ChainIDs()
	myChainID := hex.EncodeToString(s.chainID)

	if _, participate := chains[myChainID]; participate {
		// Send CIRC to other participants
		s.sendCIRCToPeers(ctx, xtReq, xtID)

		// Vote
		s.mu.RLock()
		already := s.voted[xtID.Hex()]
		s.mu.RUnlock()

		if !already {
			vote := &pb.Vote{
				SenderChainId: s.chainID,
				XtId:          xtID,
				Vote:          true,
			}
			vmsg := &pb.Message{
				SenderId: s.name,
				Payload:  &pb.Message_Vote{Vote: vote},
			}

			if err := s.spClient.Send(ctx, vmsg); err != nil {
				return err
			}

			s.log.Info().
				Str("xt_id", xtID.Hex()).
				Msg("✅ Vote sent to SP")

			s.mu.Lock()
			s.voted[xtID.Hex()] = true
			s.xtID = xtID
			s.mu.Unlock()
		}
	}

	return nil
}

func (s *Sequencer) forwardCIRCToSP(ctx context.Context, circ *pb.CIRCMessage) {
	msg := &pb.Message{
		SenderId: s.name,
		Payload:  &pb.Message_CircMessage{CircMessage: circ},
	}

	if err := s.spClient.Send(ctx, msg); err != nil {
		s.log.Error().Err(err).Msg("Failed to forward CIRC to SP")
	} else {
		s.log.Debug().
			Str("xt_id", circ.XtId.Hex()).
			Str("source", fmt.Sprintf("%x", circ.SourceChain)).
			Msg("Forwarded CIRC to SP for recording")
	}
}

func (s *Sequencer) sendCIRCToPeers(ctx context.Context, xtReq *pb.XTRequest, xtID *pb.XtID) {
	myChainID := hex.EncodeToString(s.chainID)

	for _, tx := range xtReq.Transactions {
		peerChainID := hex.EncodeToString(tx.ChainId)

		if peerChainID == myChainID {
			continue
		}

		// Create CIRC message
		circ := &pb.CIRCMessage{
			SourceChain:      s.chainID,
			DestinationChain: tx.ChainId,
			Source:           [][]byte{[]byte("0xABCD")},
			Receiver:         [][]byte{[]byte("0x1234")},
			XtId:             xtID,
			Label:            "cross-chain-call",
			Data:             [][]byte{[]byte(fmt.Sprintf("data-%s-to-%s", myChainID, peerChainID))},
		}

		// Send to peer
		if peer, exists := s.peers[peerChainID]; exists {
			msg := &pb.Message{
				SenderId: s.name,
				Payload:  &pb.Message_CircMessage{CircMessage: circ},
			}

			if err := peer.Send(ctx, msg); err != nil {
				s.log.Error().
					Str("peer", peerChainID).
					Err(err).
					Msg("Failed to send CIRC")
			} else {
				s.log.Info().
					Str("to", peerChainID).
					Str("xt_id", xtID.Hex()).
					Msg("📤 Sent CIRC to peer")
			}
		}

		// Also forward to SP for consensus coordination
		s.forwardCIRCToSP(ctx, circ)
	}
}

func (s *Sequencer) GetStats() map[string]interface{} {
	stats := s.coordinator.GetStats()
	stats["name"] = s.name
	stats["sp_connected"] = s.spClient.IsConnected()
	stats["peers"] = len(s.peers)
	return stats
}
