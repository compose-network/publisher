package adapter

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	pb "github.com/compose-network/publisher/proto/rollup/v1"
	"github.com/compose-network/publisher/x/consensus"
	"github.com/compose-network/publisher/x/publisher"
	"github.com/compose-network/publisher/x/superblock"
	"github.com/compose-network/publisher/x/superblock/l1"
	l1contracts "github.com/compose-network/publisher/x/superblock/l1/contracts"
	"github.com/compose-network/publisher/x/superblock/proofs"
	"github.com/compose-network/publisher/x/superblock/proofs/collector"
	"github.com/compose-network/publisher/x/superblock/queue"
	sreg "github.com/compose-network/publisher/x/superblock/registry"
	"github.com/compose-network/publisher/x/superblock/store"
	"github.com/compose-network/publisher/x/superblock/wal"
	"github.com/compose-network/publisher/x/transport"
	"github.com/rs/zerolog"
)

// SuperblockPublisher wraps the base publisher with SBCP capabilities
type SuperblockPublisher struct {
	publisher.Publisher
	coordinator *superblock.Coordinator
	log         zerolog.Logger

	mu             sync.RWMutex
	slotManagedXTs map[string]bool

	l2BlockStore    store.L2BlockStore
	superblockStore store.SuperblockStore
	xtQueue         queue.XTRequestQueue
	l1Publisher     l1.Publisher
	walManager      wal.Manager
	registryService sreg.Service

	// allowlist of accepted chain-ids (string(key) of []byte big-endian)
	// when empty or nil, filtering is disabled
	allowedChains map[string]struct{}
}

// WrapPublisher creates a new SuperblockPublisher by wrapping an existing publisher
func WrapPublisher(
	pub publisher.Publisher,
	config superblock.Config,
	log zerolog.Logger,
	consensusCoord consensus.Coordinator,
	transport transport.Server,
	collector collector.Service,
	prover proofs.ProverClient,
	regSvc sreg.Service,
) (*SuperblockPublisher, error) {
	if config.L1.ChainID == 0 {
		return nil, fmt.Errorf("l1.chain_id must be provided and non-zero")
	}
	l2BlockStore := store.NewMemoryL2BlockStore()
	superblockStore := store.NewMemorySuperblockStore()
	xtQueue := queue.NewMemoryXTRequestQueue(queue.DefaultConfig())

	// Build L1 publisher from config if enabled
	var l1Pub l1.Publisher
	if config.L1.Enabled {
		if config.L1.RPCEndpoint == "" || config.L1.DisputeGameFactory == "" {
			return nil, fmt.Errorf("missing L1 config: rpc_endpoint and dispute_game_factory are required")
		}
		binding, err := l1contracts.NewDisputeGameFactoryBinding(config.L1.DisputeGameFactory)
		if err != nil {
			return nil, fmt.Errorf("create L1 binding: %w", err)
		}
		l1Pub, err = l1.NewEthPublisher(context.Background(), config.L1, binding, nil, log)
		if err != nil {
			return nil, fmt.Errorf("init L1 publisher: %w", err)
		}
		log.Info().Msg("L1 publisher enabled")
	} else {
		log.Warn().Msg("L1 publisher disabled - running in test mode")
	}

	// Create the coordinator with all dependencies
	coordinator := superblock.NewCoordinator(
		config,
		log,
		nil, // metrics
		regSvc,
		l2BlockStore,
		superblockStore,
		xtQueue,
		l1Pub,
		nil, // wal
		consensusCoord,
		transport,
		collector,
		prover,
	)

	wrapper := &SuperblockPublisher{
		Publisher:       pub,
		coordinator:     coordinator,
		log:             log.With().Str("component", "sb-wrapper").Logger(),
		slotManagedXTs:  make(map[string]bool),
		l2BlockStore:    l2BlockStore,
		superblockStore: superblockStore,
		xtQueue:         xtQueue,
		l1Publisher:     l1Pub,
		walManager:      nil,
		registryService: regSvc,
		allowedChains:   nil,
	}

	// Build a simple allowlist of active chains from registry; if it fails, leave filtering disabled
	if regSvc != nil {
		if ids, err := regSvc.GetActiveRollups(context.Background()); err != nil {
			log.Warn().Err(err).Msg("Failed to load active rollups for allowlist; chain-id filtering disabled")
		} else if len(ids) > 0 {
			m := make(map[string]struct{}, len(ids))
			for _, id := range ids {
				m[string(id)] = struct{}{}
			}
			wrapper.allowedChains = m
			log.Info().Int("chains", len(m)).Msg("Chain-id allowlist enabled")
		}
	}

	// Register SBCP-specific handlers with the publisher's router
	router := pub.MessageRouter()

	// First unregister the base publisher's XTRequest handler to avoid conflicts
	// TODO: Remove this once we have a better way to handle this
	router.Unregister(publisher.XTRequestType)

	// Override XTRequest handler to use SBCP slot queue
	router.Register(publisher.XTRequestType, wrapper.handleXTRequest)

	// Register L2Block handler for SBCP
	router.Register(publisher.L2BlockType, wrapper.handleL2Block)

	// Register CIRC message handler
	router.Register(reflect.TypeOf((*pb.Message_CircMessage)(nil)), wrapper.handleCIRCMessage)

	// Register Vote handler to route to coordinator
	router.Register(publisher.VoteType, wrapper.handleVote)

	// Route consensus callbacks to SBCP coordinator
	consensusCoord.SetVoteCallback(wrapper.handleConsensusVote)
	consensusCoord.SetDecisionCallback(wrapper.handleConsensusDecision)

	return wrapper, nil
}

// queueXTRequest queues XT request in SBCP queue for slot processing
func (sp *SuperblockPublisher) queueXTRequest(ctx context.Context, from string, xtReq *pb.XTRequest) error {
	xtID, _ := xtReq.XtID()
	sp.log.Info().
		Str("xt_id", xtID.Hex()).
		Str("from", from).
		Msg("Queueing XT request for slot processing")

	// Mark as slot-managed
	sp.mu.Lock()
	sp.slotManagedXTs[xtID.Hex()] = true
	sp.mu.Unlock()

	// Queue in SBCP coordinator
	return sp.coordinator.SubmitXTRequest(ctx, from, xtReq)
}

// Start starts both the base publisher and superblock coordinator
func (sp *SuperblockPublisher) Start(ctx context.Context) error {
	if err := sp.Publisher.Start(ctx); err != nil {
		return fmt.Errorf("failed to start base publisher: %w", err)
	}
	if err := sp.coordinator.Start(ctx); err != nil {
		return fmt.Errorf("failed to start coordinator: %w", err)
	}
	return nil
}

// Stop stops both components
func (sp *SuperblockPublisher) Stop(ctx context.Context) error {
	if err := sp.coordinator.Stop(ctx); err != nil {
		return err
	}
	return sp.Publisher.Stop(ctx)
}

// GetStats combines stats from both components
func (sp *SuperblockPublisher) GetStats() map[string]interface{} {
	stats := sp.Publisher.GetStats()
	sbStats := sp.coordinator.GetStats()
	for k, v := range sbStats {
		stats["sb_"+k] = v
	}
	return stats
}

// SubmitXTRequest queues a cross-chain transaction request
func (sp *SuperblockPublisher) SubmitXTRequest(ctx context.Context, from string, request *pb.XTRequest) error {
	return sp.coordinator.SubmitXTRequest(ctx, from, request)
}

// Handler methods for router-based dispatch

func (sp *SuperblockPublisher) handleXTRequest(ctx context.Context, from string, msg *pb.Message) error {
	payload, ok := msg.Payload.(*pb.Message_XtRequest)
	if !ok {
		return fmt.Errorf("invalid payload type for XTRequest")
	}
	// filter unknown chain-ids early to prevent failing slots later on
	if len(sp.allowedChains) > 0 {
		for _, tx := range payload.XtRequest.Transactions {
			if _, ok := sp.allowedChains[string(tx.ChainId)]; !ok {
				sp.log.Warn().
					Str("from", from).
					Str("chain", fmt.Sprintf("%x", tx.ChainId)).
					Str("sender_id", msg.SenderId).
					Msg("Rejecting XTRequest: unknown chain-id")
				return nil
			}
		}
	}
	return sp.queueXTRequest(ctx, from, payload.XtRequest)
}

func (sp *SuperblockPublisher) handleL2Block(ctx context.Context, from string, msg *pb.Message) error {
	payload, ok := msg.Payload.(*pb.Message_L2Block)
	if !ok {
		return fmt.Errorf("invalid payload type for L2Block")
	}
	if len(sp.allowedChains) > 0 {
		if _, ok := sp.allowedChains[string(payload.L2Block.ChainId)]; !ok {
			sp.log.Warn().
				Str("from", from).
				Str("chain", fmt.Sprintf("%x", payload.L2Block.ChainId)).
				Str("sender_id", msg.SenderId).
				Msg("Rejecting L2Block: unknown chain-id")
			return nil
		}
	}
	return sp.coordinator.HandleL2Block(ctx, from, payload.L2Block)
}

func (sp *SuperblockPublisher) handleCIRCMessage(ctx context.Context, from string, msg *pb.Message) error {
	payload, ok := msg.Payload.(*pb.Message_CircMessage)
	if !ok {
		return fmt.Errorf("invalid payload type for CIRCMessage")
	}

	if len(sp.allowedChains) > 0 {
		if _, ok := sp.allowedChains[string(payload.CircMessage.SourceChain)]; !ok {
			sp.log.Warn().
				Str("from", from).
				Str("source_chain", fmt.Sprintf("%x", payload.CircMessage.SourceChain)).
				Str("dest_chain", fmt.Sprintf("%x", payload.CircMessage.DestinationChain)).
				Str("xt_id", payload.CircMessage.XtId.Hex()).
				Str("sender_id", msg.SenderId).
				Msg("Rejecting CIRC message: unknown source chain-id")
			return nil
		}
		if _, ok := sp.allowedChains[string(payload.CircMessage.DestinationChain)]; !ok {
			sp.log.Warn().
				Str("from", from).
				Str("source_chain", fmt.Sprintf("%x", payload.CircMessage.SourceChain)).
				Str("dest_chain", fmt.Sprintf("%x", payload.CircMessage.DestinationChain)).
				Str("xt_id", payload.CircMessage.XtId.Hex()).
				Str("sender_id", msg.SenderId).
				Msg("Rejecting CIRC message: unknown destination chain-id")
			return nil
		}
	}

	sp.log.Debug().
		Str("from", from).
		Str("source", fmt.Sprintf("%x", payload.CircMessage.SourceChain)).
		Str("dest", fmt.Sprintf("%x", payload.CircMessage.DestinationChain)).
		Str("xt_id", payload.CircMessage.XtId.Hex()).
		Msg("CIRC message observed")

	return sp.coordinator.Consensus().RecordCIRCMessage(payload.CircMessage)
}

func (sp *SuperblockPublisher) handleVote(ctx context.Context, from string, msg *pb.Message) error {
	payload, ok := msg.Payload.(*pb.Message_Vote)
	if !ok {
		return fmt.Errorf("invalid payload type for Vote")
	}

	if len(sp.allowedChains) > 0 {
		if _, ok := sp.allowedChains[string(payload.Vote.SenderChainId)]; !ok {
			sp.log.Warn().
				Str("from", from).
				Str("chain", fmt.Sprintf("%x", payload.Vote.SenderChainId)).
				Str("xt_id", payload.Vote.XtId.Hex()).
				Bool("vote", payload.Vote.Vote).
				Msg("Rejecting Vote: unknown chain-id")
			return nil
		}
	}

	sp.log.Info().
		Str("from", from).
		Str("xt_id", payload.Vote.XtId.Hex()).
		Bool("vote", payload.Vote.Vote).
		Msg("Routing vote to coordinator")

	chainID := consensus.ChainKeyBytes(payload.Vote.SenderChainId)
	decision, err := sp.coordinator.Consensus().RecordVote(payload.Vote.XtId, chainID, payload.Vote.Vote)
	if err != nil {
		return err
	}
	sp.log.Info().
		Str("xt_id", payload.Vote.XtId.Hex()).
		Str("chain", chainID).
		Str("decision_state", decision.String()).
		Msg("Vote processed by coordinator")
	return nil
}

// Consensus callbacks - route to SBCP coordinator
func (sp *SuperblockPublisher) handleConsensusVote(ctx context.Context, xtID *pb.XtID, vote bool) error {
	sp.log.Info().Str("xt_id", xtID.Hex()).Bool("vote", vote).Msg("SP broadcasting vote")
	voteMsg := &pb.Message{
		SenderId: "publisher",
		Payload: &pb.Message_Vote{
			Vote: &pb.Vote{
				SenderChainId: []byte("publisher"),
				XtId:          xtID,
				Vote:          vote,
			},
		},
	}
	return sp.coordinator.Transport().Broadcast(ctx, voteMsg, "")
}

func (sp *SuperblockPublisher) handleConsensusDecision(ctx context.Context, xtID *pb.XtID, decision bool) error {
	sp.log.Info().Str("xt_id", xtID.Hex()).Bool("decision", decision).Msg("SP broadcasting decision")
	decidedMsg := &pb.Message{
		SenderId: "publisher",
		Payload: &pb.Message_Decided{
			Decided: &pb.Decided{
				XtId:     xtID,
				Decision: decision,
			},
		},
	}
	if err := sp.coordinator.Transport().Broadcast(ctx, decidedMsg, ""); err != nil {
		sp.log.Error().Err(err).Msg("Failed to broadcast decision")
	}

	// Update SBCP slot state machine
	if err := sp.coordinator.StateMachine().ProcessSCPDecision(xtID.Hash, decision); err != nil {
		sp.log.Error().Err(err).Str("xt_id", xtID.Hex()).Msg("Failed to update SCP state")
	}

	sp.mu.Lock()
	delete(sp.slotManagedXTs, xtID.Hex())
	sp.mu.Unlock()
	return nil
}
