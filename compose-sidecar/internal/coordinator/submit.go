package coordinator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"time"

	"github.com/compose-network/compose-sdk/protocol"
	"github.com/compose-network/compose-sidecar/internal/types"
	"github.com/compose-network/specs/compose"
	"github.com/compose-network/specs/compose/proto"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	goproto "google.golang.org/protobuf/proto"
)

// PublisherClient defines the interface for publisher communication.
type PublisherClient interface {
	Connect(ctx context.Context) error
	ConnectWithRetry(ctx context.Context) error
	Disconnect(ctx context.Context) error
	SendVote(ctx context.Context, instanceID []byte, vote bool) error
	SendRaw(ctx context.Context, data []byte) error
	IsConnected() bool
	SetOnStart(fn StartCallback)
	SetOnDecision(fn VoteCallback)
}

// VoteCallback is called when a 2PC decision is received.
type VoteCallback func(ctx context.Context, instanceID string, decision bool) error

// StartCallback is called when a new cross-chain transaction starts (StartInstance).
type StartCallback func(ctx context.Context, msg *proto.StartInstance) error

// PeriodCallback is called when a new period starts (StartPeriod).
type PeriodCallback func(ctx context.Context, periodID, superblockNum uint64) error

// SubmitXT submits a cross-chain transaction.
// If a publisher client is configured and connected, SubmitXT forwards an
// XTRequest and waits for the publisher-assigned instance ID.
// If a publisher client is configured but disconnected, SubmitXT returns an
// error.
// If no publisher client is configured, SubmitXT uses standalone mode.
func (c *DefaultCoordinator) SubmitXT(ctx context.Context, id string, txs map[compose.ChainID][][]byte) (string, error) {
	cleanTxs := make(map[compose.ChainID][][]byte)
	for chainID, chainTxs := range txs {
		if len(chainTxs) == 0 {
			continue
		}
		cleanTxs[chainID] = chainTxs
	}
	if len(cleanTxs) == 0 {
		return "", fmt.Errorf("no transactions provided")
	}

	if c.publisher != nil && !c.isPublisherConnected() {
		return "", fmt.Errorf("publisher is configured but not connected")
	}

	if c.isPublisherConnected() {
		xtRequest := buildXTRequest(cleanTxs)
		requestKey := xtRequestFingerprint(xtRequest)
		waiter := c.registerSubmissionWaiter(requestKey)

		if err := c.sendXTRequestToPublisher(ctx, xtRequest); err != nil {
			c.removeSubmissionWaiter(requestKey, waiter)
			return "", err
		}

		select {
		case instanceID := <-waiter:
			if instanceID == "" {
				return "", fmt.Errorf("publisher returned empty instance_id")
			}
			return instanceID, nil
		case <-ctx.Done():
			c.removeSubmissionWaiter(requestKey, waiter)
			return "", ctx.Err()
		}
	}

	c.mu.Lock()

	if id == "" {
		c.originSeq++
		id = fmt.Sprintf("xt-%d-%d", c.chainID, c.originSeq)
	}
	originSeq := c.originSeq

	if _, exists := c.pending[id]; exists {
		c.mu.Unlock()
		return "", fmt.Errorf("XT %s already pending", id)
	}

	txMap := make(map[compose.ChainID][]*ethtypes.Transaction)
	for chainID, chainTxs := range cleanTxs {
		for _, txBytes := range chainTxs {
			tx := new(ethtypes.Transaction)
			if err := tx.UnmarshalBinary(txBytes); err != nil {
				c.mu.Unlock()
				return "", fmt.Errorf("failed to decode transaction for chain %d: %w", chainID, err)
			}
			txMap[chainID] = append(txMap[chainID], tx)
		}
	}

	xt := &types.PendingXT{
		ID:             id,
		InstanceID:     []byte(id),
		Transactions:   txMap,
		RawTxs:         cleanTxs,
		ChainStates:    make(map[compose.ChainID]*protocol.ChainState),
		StateOverrides: make(map[compose.ChainID]map[string]any),
		PeerVotes:      make(map[compose.ChainID]bool),
		CreatedAt:      time.Now(),
		OriginChain:    c.chainID,
		OriginSeq:      originSeq,
		LockedChains:   make(map[compose.ChainID]bool),
	}

	c.pending[id] = xt
	c.waiters[id] = make(map[compose.ChainID]chan *protocol.BuilderPollResponse)

	c.log.Info().
		Str("xt_id", id).
		Int("chains", len(txs)).
		Msg("New XT submitted via API")

	c.mu.Unlock()

	if c.peerCoordinator != nil {
		forwardCtxBase := ctx
		if forwardCtxBase == nil {
			forwardCtxBase = context.Background()
		}
		go func(originSeq compose.SequenceNumber) {
			forwardCtx, cancel := context.WithTimeout(context.WithoutCancel(forwardCtxBase), 5*time.Second)
			defer cancel()
			if err := c.peerCoordinator.ForwardXT(forwardCtx, id, txs, originSeq); err != nil {
				c.log.Error().Err(err).Str("xt_id", id).Msg("Failed to forward XT to peers")
			} else {
				c.log.Info().Str("xt_id", id).Msg("Forwarded XT to peer sidecars")
			}
		}(originSeq)
	}

	return id, nil
}

func (c *DefaultCoordinator) sendXTRequestToPublisher(ctx context.Context, xtRequest *proto.XTRequest) error {
	if xtRequest == nil {
		return fmt.Errorf("nil XTRequest")
	}

	c.log.Info().
		Int("chains", len(xtRequest.GetTransactionRequests())).
		Msg("Sending XTRequest to publisher")

	msg := &proto.Message{
		Payload: &proto.Message_XtRequest{XtRequest: xtRequest},
	}

	data, err := goproto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal XTRequest: %w", err)
	}

	if err := c.publisher.SendRaw(ctx, data); err != nil {
		return fmt.Errorf("send XTRequest to publisher: %w", err)
	}

	return nil
}

func buildXTRequest(txs map[compose.ChainID][][]byte) *proto.XTRequest {
	if len(txs) == 0 {
		return &proto.XTRequest{}
	}

	chainIDs := make([]compose.ChainID, 0, len(txs))
	for chainID := range txs {
		chainIDs = append(chainIDs, chainID)
	}
	slices.Sort(chainIDs)

	txRequests := make([]*proto.TransactionRequest, 0, len(chainIDs))
	for _, chainID := range chainIDs {
		chainTxs := txs[chainID]
		if len(chainTxs) == 0 {
			continue
		}
		txRequests = append(txRequests, &proto.TransactionRequest{
			ChainId:     uint64(chainID),
			Transaction: chainTxs,
		})
	}

	return &proto.XTRequest{TransactionRequests: txRequests}
}

func xtRequestFingerprint(req *proto.XTRequest) string {
	if req == nil {
		return ""
	}
	data, err := goproto.Marshal(req)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:16])
}

func (c *DefaultCoordinator) registerSubmissionWaiter(requestKey string) chan string {
	waiter := make(chan string, 1)
	c.mu.Lock()
	c.submissionWaiters[requestKey] = append(c.submissionWaiters[requestKey], waiter)
	c.mu.Unlock()
	return waiter
}

func (c *DefaultCoordinator) removeSubmissionWaiter(requestKey string, waiter chan string) {
	c.mu.Lock()
	waiters := c.submissionWaiters[requestKey]
	for i, ch := range waiters {
		if ch == waiter {
			waiters = append(waiters[:i], waiters[i+1:]...)
			break
		}
	}
	if len(waiters) == 0 {
		delete(c.submissionWaiters, requestKey)
	} else {
		c.submissionWaiters[requestKey] = waiters
	}
	c.mu.Unlock()
}

func (c *DefaultCoordinator) resolveSubmissionWaiter(requestKey, instanceID string) {
	c.mu.Lock()
	waiters := c.submissionWaiters[requestKey]
	if len(waiters) == 0 {
		c.mu.Unlock()
		return
	}
	waiter := waiters[0]
	if len(waiters) == 1 {
		delete(c.submissionWaiters, requestKey)
	} else {
		c.submissionWaiters[requestKey] = waiters[1:]
	}
	c.mu.Unlock()

	select {
	case waiter <- instanceID:
	default:
	}
	close(waiter)
}
