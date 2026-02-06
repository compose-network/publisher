package coordinator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/compose-network/compose-sdk/mailbox"
	"github.com/compose-network/compose-sdk/peer"
	"github.com/compose-network/compose-sdk/protocol"
	"github.com/compose-network/compose-sidecar/internal/types"
	"github.com/compose-network/specs/compose"
	"github.com/compose-network/specs/compose/proto"
	"github.com/rs/zerolog"
)

const (
	defaultPollInterval = 50 * time.Millisecond
	defaultBuildTimeout = 150 * time.Millisecond
	defaultCIRCTimeout  = 10 * time.Second
	maxResimulations    = 3
)

// Coordinator defines the interface for cross-chain transaction coordination.
type Coordinator interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error

	HandleBuilderPoll(ctx context.Context, req *protocol.BuilderPollRequest) (*protocol.BuilderPollResponse, error)
	SubmitXT(ctx context.Context, id string, txs map[compose.ChainID][][]byte) (string, error)
	HandleStartInstance(ctx context.Context, msg *proto.StartInstance) error
	HandleStartPeriod(ctx context.Context, periodID compose.PeriodID, superblockNum compose.SuperblockNumber) error
	OnDecision(ctx context.Context, instanceID string, decision bool) error
	HandleMailboxMessage(ctx context.Context, msg *proto.MailboxMessage) error
	Cleanup(maxAge time.Duration)
	HandlePeerVote(ctx context.Context, instanceID string, chainID compose.ChainID, vote bool) error
	HandleForwardedXT(ctx context.Context, instanceID string, txs map[compose.ChainID][][]byte, originChain compose.ChainID, originSeq compose.SequenceNumber) error
	HandleRollback(ctx context.Context, periodID compose.PeriodID, lastFinalizedSuperblockNum uint64, lastFinalizedSuperblockHash []byte) error
	AckDelivery(ctx context.Context, chainID compose.ChainID, instanceIDs []string) error
	GetXTStatus(ctx context.Context, instanceID string) (*XTStatusResponse, error)
}

// XTStatusResponse represents the response for an XT status query.
type XTStatusResponse struct {
	InstanceID string            `json:"instance_id"`
	Status     protocol.XTStatus `json:"status"`
	Decision   *bool             `json:"decision,omitempty"`
}

// DefaultCoordinator is the production implementation of Coordinator.
type DefaultCoordinator struct {
	mu      sync.RWMutex
	log     zerolog.Logger
	chainID compose.ChainID

	pending           map[string]*types.PendingXT
	chainStates       map[compose.ChainID]*protocol.ChainState
	waiters           map[string]map[compose.ChainID]chan *protocol.BuilderPollResponse
	submissionWaiters map[string][]chan string

	simulator       Simulator
	publisher       PublisherClient
	mailboxSender   MailboxSender
	mailboxQueue    mailbox.Queue
	peerCoordinator peer.Coordinator
	putInboxBuilder PutInboxBuilder
	nonceManager    *DeferredNonceManager

	peerVotes map[string]map[compose.ChainID]bool

	currentPeriodID      compose.PeriodID
	currentSuperblockNum compose.SuperblockNumber
	periodInitialized    bool
	lastSequenceNum      compose.SequenceNumber

	originSeq compose.SequenceNumber

	chainOverlays map[compose.ChainID]*chainOverlay
	chainLocks    map[compose.ChainID]*sync.Mutex

	circTimeout     time.Duration
	lastKnownBlocks map[compose.ChainID]uint64

	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// CoordinatorConfig holds all dependencies for creating a DefaultCoordinator.
type CoordinatorConfig struct {
	ChainID         compose.ChainID
	Simulator       Simulator
	Publisher       PublisherClient
	MailboxSender   MailboxSender
	MailboxQueue    mailbox.Queue
	PeerCoordinator peer.Coordinator
	PutInboxBuilder PutInboxBuilder
	CIRCTimeout     time.Duration
	Log             zerolog.Logger
}

// NewCoordinator creates a new DefaultCoordinator from the given config.
func NewCoordinator(cfg CoordinatorConfig) *DefaultCoordinator {
	circTimeout := cfg.CIRCTimeout
	if circTimeout == 0 {
		circTimeout = defaultCIRCTimeout
	}

	return &DefaultCoordinator{
		log:               cfg.Log.With().Str("component", "coordinator").Logger(),
		chainID:           cfg.ChainID,
		pending:           make(map[string]*types.PendingXT),
		chainStates:       make(map[compose.ChainID]*protocol.ChainState),
		waiters:           make(map[string]map[compose.ChainID]chan *protocol.BuilderPollResponse),
		submissionWaiters: make(map[string][]chan string),
		simulator:         cfg.Simulator,
		publisher:         cfg.Publisher,
		mailboxSender:     cfg.MailboxSender,
		mailboxQueue:      cfg.MailboxQueue,
		peerCoordinator:   cfg.PeerCoordinator,
		putInboxBuilder:   cfg.PutInboxBuilder,
		nonceManager:      NewDeferredNonceManager(),
		peerVotes:         make(map[string]map[compose.ChainID]bool),
		chainOverlays:     make(map[compose.ChainID]*chainOverlay),
		chainLocks:        make(map[compose.ChainID]*sync.Mutex),
		circTimeout:       circTimeout,
		lastKnownBlocks:   make(map[compose.ChainID]uint64),
		stopCh:            make(chan struct{}),
	}
}

// Start starts the coordinator's background goroutines.
func (c *DefaultCoordinator) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("coordinator already running")
	}

	c.log.Info().Uint64("chain_id", uint64(c.chainID)).Msg("Starting coordinator")
	c.running = true

	c.wg.Add(1)
	go c.cleanupLoop()

	return nil
}

func (c *DefaultCoordinator) cleanupLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.Cleanup(5 * time.Minute)
		}
	}
}

// Stop gracefully shuts down the coordinator.
func (c *DefaultCoordinator) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	c.log.Info().Msg("Stopping coordinator")
	close(c.stopCh)
	c.wg.Wait()
	c.running = false

	return nil
}

// Cleanup removes decided XTs older than maxAge.
func (c *DefaultCoordinator) Cleanup(maxAge time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for id, xt := range c.pending {
		if xt.Decision != nil && now.Sub(xt.DecidedAt) > maxAge {
			delete(c.pending, id)
			delete(c.waiters, id)
		}
	}
}

// GetXTStatus retrieves the current status of a cross-chain transaction.
func (c *DefaultCoordinator) GetXTStatus(ctx context.Context, instanceID string) (*XTStatusResponse, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	xt, exists := c.pending[instanceID]
	if !exists {
		return nil, fmt.Errorf("XT not found: %s", instanceID)
	}

	status := c.determineXTStatus(xt)

	return &XTStatusResponse{
		InstanceID: instanceID,
		Status:     status,
		Decision:   xt.Decision,
	}, nil
}

func (c *DefaultCoordinator) determineXTStatus(xt *types.PendingXT) protocol.XTStatus {
	if xt.Decision != nil {
		if *xt.Decision {
			return protocol.XTStatusCommitted
		}
		return protocol.XTStatusAborted
	}
	if xt.VoteSent {
		return protocol.XTStatusVoted
	}
	if !xt.SimulatedAt.IsZero() {
		if len(xt.Dependencies) > 0 && len(xt.FulfilledDeps) < len(xt.Dependencies) {
			return protocol.XTStatusWaitingCIRC
		}
		return protocol.XTStatusSimulated
	}
	if len(xt.ChainStates) > 0 {
		return protocol.XTStatusSimulating
	}
	return protocol.XTStatusPending
}
