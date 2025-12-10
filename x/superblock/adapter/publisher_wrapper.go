package adapter

import (
	"context"
	"fmt"
	"sync"

	"github.com/compose-network/publisher/x/consensus"
	"github.com/compose-network/publisher/x/superblock"
	"github.com/compose-network/publisher/x/superblock/l1"
	l1contracts "github.com/compose-network/publisher/x/superblock/l1/contracts"
	"github.com/compose-network/publisher/x/superblock/proofs"
	"github.com/compose-network/publisher/x/superblock/proofs/collector"
	"github.com/compose-network/publisher/x/superblock/queue"
	sreg "github.com/compose-network/publisher/x/superblock/registry"
	"github.com/compose-network/publisher/x/superblock/sequencer"
	"github.com/compose-network/publisher/x/superblock/store"
	"github.com/compose-network/publisher/x/superblock/wal"
	"github.com/compose-network/publisher/x/transport"
	"github.com/compose-network/specs/compose"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/compose-network/specs/compose/sbcp"
	"github.com/rs/zerolog"
)

// SuperblockPublisher wraps the base publisher with SBCP capabilities
type SuperblockPublisher struct {
	publisher   sbcp.Publisher
	coordinator sequencer.Coordinator
	log         zerolog.Logger

	mu             sync.RWMutex
	slotManagedXTs map[compose.InstanceID]bool

	superblockStore store.SuperblockStore
	xtQueue         queue.XTRequestQueue
	l1Publisher     l1.Publisher
	walManager      wal.Manager
	registryService sreg.Service

	// allowlist of accepted chain-ids
	// when empty or nil, filtering is disabled
	allowedChains map[compose.ChainID]struct{}
}

// WrapPublisher creates a new SuperblockPublisher by wrapping an existing publisher
func WrapPublisher(
	pub sbcp.Publisher,
	config superblock.Config,
	log zerolog.Logger,
	consensusCoord consensus.Coordinator,
	sequencerCoordinator sequencer.Coordinator,
	transport transport.Server,
	collector collector.Service,
	prover proofs.ProverClient,
	regSvc sreg.Service,
) (*SuperblockPublisher, error) {
	if config.L1.ChainID == 0 {
		return nil, fmt.Errorf("l1.chain_id must be provided and non-zero")
	}
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

	wrapper := &SuperblockPublisher{
		publisher:       pub,
		coordinator:     sequencerCoordinator,
		log:             log.With().Str("component", "sb-wrapper").Logger(),
		slotManagedXTs:  make(map[compose.InstanceID]bool),
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
			m := make(map[compose.ChainID]struct{}, len(ids))
			for _, id := range ids {
				m[id] = struct{}{}
			}
			wrapper.allowedChains = m
			log.Info().Int("chains", len(m)).Msg("Chain-id allowlist enabled")
		}
	}

	// // Register SBCP-specific handlers with the publisher's router
	// router := pub.MessageRouter()

	// // First unregister the base publisher's XTRequest handler to avoid conflicts
	// // TODO: Remove this once we have a better way to handle this
	// router.Unregister(publisher.XTRequestType)

	// // Override XTRequest handler to use SBCP slot queue
	// router.Register(publisher.XTRequestType, wrapper.handleXTRequest)

	// // Register CIRC message handler
	// router.Register(reflect.TypeOf((*pb.Message_MailboxMessage)(nil)), wrapper.handleMailboxMessage)

	// // Register Vote handler to route to coordinator
	// router.Register(publisher.VoteType, wrapper.handleVote)

	// Route consensus callbacks to SBCP coordinator
	consensusCoord.SetVoteCallback(wrapper.handleConsensusVote)
	consensusCoord.SetDecisionCallback(wrapper.handleConsensusDecision)

	return wrapper, nil
}

// Stop stops coordinator
func (sp *SuperblockPublisher) Stop(ctx context.Context) error {
	if err := sp.coordinator.Stop(ctx); err != nil {
		return err
	}

	return nil
}

// SubmitXTRequest queues a cross-chain transaction request
func (sp *SuperblockPublisher) SubmitXTRequest(ctx context.Context, from string, request *pb.XTRequest) error {
	return sp.coordinator.SubmitXTRequest(ctx, from, request)
}

// Consensus callbacks - route to SBCP coordinator
func (sp *SuperblockPublisher) handleConsensusVote(
	ctx context.Context, instanceID compose.InstanceID, vote bool,
) error {
	sp.log.Info().Str("instance_id", instanceID.String()).Bool("vote", vote).Msg("SP broadcasting vote")
	voteMsg := &pb.Message{
		SenderId: "publisher",
		Payload: &pb.Message_Vote{
			Vote: &pb.Vote{
				ChainId:    0, //TODO: set proper chain id
				InstanceId: instanceID[:],
				Vote:       vote,
			},
		},
	}

	return sp.coordinator.Transport().Broadcast(ctx, voteMsg, "")
}

func (sp *SuperblockPublisher) handleConsensusDecision(
	ctx context.Context, instanceID compose.InstanceID, decision bool,
) error {
	sp.log.Info().Str("instance_id", instanceID.String()).Bool("decision", decision).Msg("SP broadcasting decision")
	decidedMsg := &pb.Message{
		SenderId: "publisher",
		Payload: &pb.Message_Decided{
			Decided: &pb.Decided{
				InstanceId: instanceID[:],
				Decision:   decision,
			},
		},
	}
	if err := sp.coordinator.Transport().Broadcast(ctx, decidedMsg, ""); err != nil {
		sp.log.Error().Err(err).Msg("Failed to broadcast decision")
	}

	//TODO: V2 migration - implement state machine?
	// // Update SBCP slot state machine
	// if err := sp.coordinator.StateMachine().ProcessSCPDecision(instanceID, decision); err != nil {
	// 	sp.log.Error().Err(err).Str("xt_id", instanceID.String()).Msg("Failed to update SCP state")
	// }

	sp.mu.Lock()
	delete(sp.slotManagedXTs, instanceID)
	sp.mu.Unlock()
	return nil
}
