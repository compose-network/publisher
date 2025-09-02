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
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/sequencer"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/sequencer/bootstrap"
)

type Sequencer struct {
	name        string
	chainID     []byte
	coordinator sequencer.Coordinator // SBCP coordinator
	runtime     *bootstrap.Runtime
	spAddr      string
	log         zerolog.Logger
	authMgr     auth.Manager

	// Channels for test coordination
	decidedCh chan *pb.Decided
	circCh    chan *pb.CIRCMessage

	// State
	mu    sync.RWMutex
	voted map[string]bool
	xtID  *pb.XtID
}

func NewSequencer(name string, chainID []byte, spAddr string, log zerolog.Logger, authMgr auth.Manager) *Sequencer {
	s := &Sequencer{
		name:      name,
		chainID:   chainID,
		spAddr:    spAddr,
		log:       log.With().Str("seq", name).Logger(),
		authMgr:   authMgr,
		decidedCh: make(chan *pb.Decided, 1),
		circCh:    make(chan *pb.CIRCMessage, 10),
		voted:     make(map[string]bool),
	}

	// Determine P2P listen addr and peers
	const defaultP2PPort = ":9000"
	chainIDStr := hex.EncodeToString(chainID)
	var p2pListen string
	peers := map[string]string{}
	switch chainIDStr {
	case "012fd1": // 77777
		p2pListen = defaultP2PPort
		peers["88888"] = "localhost:9001"
	case "015b38": // 88888
		p2pListen = ":9001"
		peers["77777"] = "localhost" + defaultP2PPort
	default:
		p2pListen = ":9002"
	}

	// Bootstrap SBCP runtime
	rt, err := bootstrap.Setup(context.Background(), bootstrap.Config{
		ChainID:         chainID,
		SPAddr:          spAddr,
		PeerAddrs:       peers,
		P2PListenAddr:   p2pListen,
		Log:             s.log,
		SlotDuration:    12 * time.Second,
		SlotSealCutover: 2.0 / 3.0,
	})
	if err != nil {
		s.log.Fatal().Err(err).Msg("bootstrap setup failed")
	}
	s.runtime = rt
	s.coordinator = rt.Coordinator

	// Intercept SP messages for test coordination; still route to coordinator
	s.runtime.SPClient.SetHandler(s.handleSPMessage)

	// Intercept P2P server messages to surface CIRC; still route to coordinator
	s.runtime.P2PServer.SetHandler(s.handleP2PMessage)

	return s
}

func (s *Sequencer) Start(ctx context.Context) error {
	if err := s.runtime.Start(ctx); err != nil {
		return fmt.Errorf("runtime start: %w", err)
	}
	s.log.Info().Str("sp", s.spAddr).Msg("Sequencer runtime started")
	return nil
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
		// Route to SBCP coordinator for any state updates
		if err := s.coordinator.HandleMessage(ctx, msg.SenderId, msg); err != nil {
			s.log.Error().Err(err).Msg("Coordinator failed to handle Decided message")
		}
		return nil, nil

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

		// Process locally
		select {
		case s.circCh <- circ:
		default:
		}

		// Route to coordinator; internal consensus handler will record it
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

	if err := s.runtime.SPClient.Send(ctx, msg); err != nil {
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

			if err := s.runtime.SPClient.Send(ctx, vmsg); err != nil {
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

func (s *Sequencer) sendCIRCToPeers(ctx context.Context, xtReq *pb.XTRequest, xtID *pb.XtID) {
	myChainID := hex.EncodeToString(s.chainID)

	for _, tx := range xtReq.Transactions {
		peerChainID := hex.EncodeToString(tx.ChainId)
		if peerChainID == myChainID {
			continue
		}
		circ := &pb.CIRCMessage{
			SourceChain:      s.chainID,
			DestinationChain: tx.ChainId,
			Source:           [][]byte{[]byte("0xABCD")},
			Receiver:         [][]byte{[]byte("0x1234")},
			XtId:             xtID,
			Label:            "cross-chain-call",
			Data:             [][]byte{[]byte(fmt.Sprintf("data-%s-to-%s", myChainID, peerChainID))},
		}
		if err := s.runtime.SendCIRC(ctx, circ); err != nil {
			s.log.Error().Str("to", peerChainID).Err(err).Msg("Failed to send CIRC")
		} else {
			s.log.Info().Str("to", peerChainID).Str("xt_id", xtID.Hex()).Msg("📤 Sent CIRC to peer")
		}
	}
}

func (s *Sequencer) GetStats() map[string]interface{} {
	stats := s.coordinator.GetStats()
	stats["name"] = s.name
	stats["sp_connected"] = s.runtime.SPClient.IsConnected()
	return stats
}
