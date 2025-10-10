package rollback

import (
	"bytes"
	"context"
	"fmt"

	"github.com/rs/zerolog"
	pb "github.com/ssvlabs/rollup-shared-publisher/proto/rollup/v1"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/registry"
	"github.com/ssvlabs/rollup-shared-publisher/x/superblock/store"
)

// Recovery handles state recovery operations during rollback
type Recovery struct {
	superblockStore store.SuperblockStore
	l2BlockStore    store.L2BlockStore
	registryService registry.Service
	log             zerolog.Logger
}

// NewRecovery creates a new recovery handler
func NewRecovery(
	superblockStore store.SuperblockStore,
	l2BlockStore store.L2BlockStore,
	registryService registry.Service,
	logger zerolog.Logger,
) *Recovery {
	return &Recovery{
		superblockStore: superblockStore,
		l2BlockStore:    l2BlockStore,
		registryService: registryService,
		log:             logger.With().Str("component", "rollback.recovery").Logger(),
	}
}

// FindLastValidSuperblock searches backwards from the rolled-back superblock number
// to find the last superblock that was not rolled back
func (r *Recovery) FindLastValidSuperblock(ctx context.Context, rolledBackNumber uint64) (*store.Superblock, error) {
	if rolledBackNumber == 0 {
		return nil, NewValidationError("invalid rolled-back number 0")
	}

	r.log.Debug().
		Uint64("rolled_back_number", rolledBackNumber).
		Msg("Searching for last valid superblock")

	// Search backwards from the rolled-back number
	for number := rolledBackNumber - 1; number > 0; number-- {
		sb, err := r.superblockStore.GetSuperblock(ctx, number)
		if err != nil {
			r.log.Debug().
				Err(err).
				Uint64("superblock_number", number).
				Msg("Could not retrieve superblock during rollback recovery")
			continue
		}

		// Check if this superblock was not rolled back
		if sb.Status != store.SuperblockStatusRolledBack {
			r.log.Info().
				Uint64("superblock_number", number).
				Str("status", string(sb.Status)).
				Msg("Found last valid superblock for rollback recovery")
			return sb, nil
		}

		r.log.Debug().
			Uint64("superblock_number", number).
			Msg("Superblock was also rolled back, continuing search")
	}

	r.log.Info().Msg("No valid superblock found, will restart from genesis")
	return nil, nil
}

// ComputeL2BlockRequests computes the L2 block requests needed to restart chains
// after a rollback, based on the last valid superblock state
func (r *Recovery) ComputeL2BlockRequests(
	ctx context.Context,
	lastValid *store.Superblock,
) ([]*pb.L2BlockRequest, error) {
	// Get the currently active rollups
	activeRollups, err := r.registryService.GetActiveRollups(ctx)
	if err != nil {
		return nil, NewRecoveryError("failed to get active rollups").WithCause(err)
	}

	r.log.Debug().
		Int("active_rollups_count", len(activeRollups)).
		Bool("has_last_valid", lastValid != nil).
		Msg("Computing L2 block requests for rollback restart")

	requests := make([]*pb.L2BlockRequest, 0, len(activeRollups))

	for _, chainID := range activeRollups {
		request := r.computeL2BlockRequestForChain(ctx, chainID, lastValid)
		requests = append(requests, request)
	}

	r.log.Info().
		Int("l2_requests_count", len(requests)).
		Msg("Computed L2 block requests for rollback restart")

	return requests, nil
}

// computeL2BlockRequestForChain computes the L2 block request for a specific chain
func (r *Recovery) computeL2BlockRequestForChain(
	ctx context.Context,
	chainID []byte,
	lastValid *store.Superblock,
) *pb.L2BlockRequest {
	var headBlock *pb.L2Block

	// First, try to find the head block from the last valid superblock
	if lastValid != nil {
		for _, l2Block := range lastValid.L2Blocks {
			if bytes.Equal(l2Block.ChainId, chainID) {
				headBlock = l2Block
				r.log.Debug().
					Str("chain_id", fmt.Sprintf("%x", chainID)).
					Uint64("block_number", l2Block.BlockNumber).
					Msg("Found head block from last valid superblock")
				break
			}
		}
	}

	// If not found in the last valid superblock, try the L2 block store
	if headBlock == nil {
		if latest, err := r.l2BlockStore.GetLatestL2Block(ctx, chainID); err == nil && latest != nil {
			headBlock = latest
			r.log.Debug().
				Str("chain_id", fmt.Sprintf("%x", chainID)).
				Uint64("block_number", latest.BlockNumber).
				Msg("Found head block from L2 block store")
		}
	}

	// Create the L2 block request
	request := &pb.L2BlockRequest{
		ChainId: append([]byte(nil), chainID...),
	}

	if headBlock != nil {
		// Request the next block after the head
		request.BlockNumber = headBlock.BlockNumber + 1
		request.ParentHash = append([]byte(nil), headBlock.BlockHash...)

		r.log.Debug().
			Str("chain_id", fmt.Sprintf("%x", chainID)).
			Uint64("requested_block_number", request.BlockNumber).
			Str("parent_hash", fmt.Sprintf("%x", request.ParentHash)).
			Msg("Created L2 block request for chain")
	} else {
		// No head block found, request genesis block
		request.BlockNumber = 0
		request.ParentHash = nil

		r.log.Warn().
			Str("chain_id", fmt.Sprintf("%x", chainID)).
			Msg("No prior L2 block found during rollback; requesting genesis block")
	}

	return request
}

// ValidateRecoveryState validates that the recovery state is consistent
func (r *Recovery) ValidateRecoveryState(
	ctx context.Context,
	lastValid *store.Superblock,
	l2Requests []*pb.L2BlockRequest,
) error {
	if lastValid == nil {
		r.log.Info().Msg("No last valid superblock, recovery state is consistent for genesis restart")
		return nil
	}

	// Validate that we have L2 requests for all chains in the last valid superblock
	for _, l2Block := range lastValid.L2Blocks {
		found := false
		for _, req := range l2Requests {
			if bytes.Equal(req.ChainId, l2Block.ChainId) {
				found = true
				// Validate the block number continuity
				if req.BlockNumber != l2Block.BlockNumber+1 {
					return NewValidationError("L2 block request number mismatch").
						WithContext("chain_id", fmt.Sprintf("%x", l2Block.ChainId)).
						WithContext("expected", l2Block.BlockNumber+1).
						WithContext("actual", req.BlockNumber)
				}
				break
			}
		}

		if !found {
			return NewValidationError("missing L2 block request for chain in last valid superblock").
				WithContext("chain_id", fmt.Sprintf("%x", l2Block.ChainId))
		}
	}

	r.log.Debug().
		Uint64("last_valid_superblock", lastValid.Number).
		Int("l2_requests", len(l2Requests)).
		Msg("Recovery state validation passed")

	return nil
}

// GetRecoveryMetrics returns metrics about the recovery operation
func (r *Recovery) GetRecoveryMetrics(lastValid *store.Superblock, rolledBackNumber uint64) map[string]interface{} {
	metrics := make(map[string]interface{})

	if lastValid != nil {
		metrics["last_valid_superblock_number"] = lastValid.Number
		metrics["superblocks_rolled_back"] = rolledBackNumber - lastValid.Number
		metrics["l2_blocks_in_last_valid"] = len(lastValid.L2Blocks)
	} else {
		metrics["last_valid_superblock_number"] = 0
		metrics["superblocks_rolled_back"] = rolledBackNumber
		metrics["l2_blocks_in_last_valid"] = 0
	}

	metrics["rolled_back_superblock_number"] = rolledBackNumber

	return metrics
}
