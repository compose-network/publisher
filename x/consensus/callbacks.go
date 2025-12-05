package consensus

import (
	"context"
	"time"

	"github.com/compose-network/specs/compose"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"

	pb "github.com/compose-network/specs/compose/proto"
)

// CallbackManager manages coordinator callbacks with error handling and timeouts
type CallbackManager struct {
	startFn    StartFn
	voteFn     VoteFn
	decisionFn DecisionFn
	blockFn    BlockFn

	timeout time.Duration
	log     zerolog.Logger
}

// NewCallbackManager creates a new callback manager
func NewCallbackManager(timeout time.Duration, log zerolog.Logger) *CallbackManager {
	return &CallbackManager{
		timeout: timeout,
		log:     log.With().Str("component", "callback-manager").Logger(),
	}
}

// SetStartCallback sets the start callback
func (cm *CallbackManager) SetStartCallback(fn StartFn) {
	cm.startFn = fn
}

// SetVoteCallback sets the vote callback
func (cm *CallbackManager) SetVoteCallback(fn VoteFn) {
	cm.voteFn = fn
}

// SetDecisionCallback sets the decision callback
func (cm *CallbackManager) SetDecisionCallback(fn DecisionFn) {
	cm.decisionFn = fn
}

// SetBlockCallback sets the block callback
func (cm *CallbackManager) SetBlockCallback(fn BlockFn) {
	cm.blockFn = fn
}

// InvokeStart calls the start callback with timeout and error handling
func (cm *CallbackManager) InvokeStart(ctx context.Context, from string, xtReq *pb.XTRequest) {
	if cm.startFn == nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(ctx, cm.timeout)
		defer cancel()

		if err := cm.startFn(ctx, from, xtReq); err != nil {
			cm.log.Error().
				Err(err).
				Str("from", from).
				Msg("Start callback failed")
		}
	}()
}

// InvokeVote calls the vote callback with timeout and error handling
func (cm *CallbackManager) InvokeVote(instanceID compose.InstanceID, vote bool, duration time.Duration) {
	if cm.voteFn == nil {
		return
	}

	cm.invokeCallback("vote", instanceID, func(ctx context.Context) error {
		return cm.voteFn(ctx, instanceID, vote)
	})
}

// InvokeDecision calls the decision callback with timeout and error handling
func (cm *CallbackManager) InvokeDecision(instanceID compose.InstanceID, decision bool, duration time.Duration) {
	if cm.decisionFn == nil {
		return
	}

	cm.invokeCallback("decision", instanceID, func(ctx context.Context) error {
		return cm.decisionFn(ctx, instanceID, decision)
	})
}

// InvokeBlock calls the block callback with timeout and error handling
func (cm *CallbackManager) InvokeBlock(ctx context.Context, block *types.Block, instanceIDs []compose.InstanceID) {
	if cm.blockFn == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(ctx, cm.timeout)
		defer cancel()
		if err := cm.blockFn(ctx, block, instanceIDs); err != nil {
			cm.log.Error().
				Err(err).
				Int("xt_count", len(instanceIDs)).
				Msg("Block callback failed")
		}
	}()
}

// invokeCallback is a helper to invoke callbacks with error handling and timeout
func (cm *CallbackManager) invokeCallback(callbackType string, xtID compose.InstanceID, fn func(context.Context) error) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), cm.timeout)
		defer cancel()

		if err := fn(ctx); err != nil {
			cm.log.Error().
				Err(err).
				Str("instance_id", xtID.String()).
				Str("type", callbackType).
				Msg("Callback failed")
		}
	}()
}
