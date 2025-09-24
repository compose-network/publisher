package batch

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"
)

// EpochEvent represents an Ethereum epoch transition event
type EpochEvent struct {
	EpochNumber uint64
	BlockNumber uint64
	BlockHash   common.Hash
	Timestamp   time.Time
}

// BatchTrigger represents a batch synchronization trigger
type BatchTrigger struct {
	TriggerEpoch uint64
	TriggerTime  time.Time
	ShouldStart  bool // true for batch start, false for batch end
}

// BeaconChainAPI handles Ethereum beacon chain API calls
type BeaconChainAPI struct {
	baseURL    string
	httpClient *http.Client
	log        zerolog.Logger
}

// BeaconEpochData represents beacon chain epoch information
type BeaconEpochData struct {
	Epoch          uint64 `json:"epoch"`
	BlockNumber    uint64 `json:"execution_block_number"`
	BlockHash      string `json:"execution_block_hash"`
	Timestamp      uint64 `json:"timestamp"`
	FinalizedEpoch uint64 `json:"finalized_epoch"`
}

// L1Listener listens to Ethereum L1 events for batch synchronization
type L1Listener struct {
	client             *ethclient.Client
	beaconAPI          *BeaconChainAPI
	log                zerolog.Logger
	batchFactor        uint64
	pollInterval       time.Duration
	lastProcessedEpoch uint64

	// Event channels
	epochCh   chan EpochEvent
	triggerCh chan BatchTrigger
	errorCh   chan error

	// Control
	ctx    context.Context
	cancel context.CancelFunc
}

// Config for L1Listener
type ListenerConfig struct {
	L1RPC        string        `mapstructure:"l1_rpc"        yaml:"l1_rpc"`
	BeaconRPC    string        `mapstructure:"beacon_rpc"    yaml:"beacon_rpc"`    // Beacon chain API endpoint
	BatchFactor  uint64        `mapstructure:"batch_factor"  yaml:"batch_factor"`  // Default: 10 (trigger every 10 epochs)
	PollInterval time.Duration `mapstructure:"poll_interval" yaml:"poll_interval"` // Default: 12 seconds
}

// DefaultListenerConfig returns sensible defaults
func DefaultListenerConfig() ListenerConfig {
	return ListenerConfig{
		L1RPC:        "https://eth-mainnet.alchemyapi.io/v2/YOUR_KEY",
		BeaconRPC:    "https://beacon-nd-123-456-789.p2pify.com",
		BatchFactor:  10,
		PollInterval: 12 * time.Second, // Ethereum slot time
	}
}

// NewL1Listener creates a new L1 listener
func NewL1Listener(cfg ListenerConfig, log zerolog.Logger) (*L1Listener, error) {
	if cfg.L1RPC == "" {
		return nil, fmt.Errorf("l1 RPC URL is required")
	}
	if cfg.BeaconRPC == "" {
		return nil, fmt.Errorf("beacon chain RPC URL is required")
	}

	client, err := ethclient.Dial(cfg.L1RPC)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to L1: %w", err)
	}

	beaconAPI := &BeaconChainAPI{
		baseURL:    cfg.BeaconRPC,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		log:        log.With().Str("component", "beacon-api").Logger(),
	}

	logger := log.With().Str("component", "l1-batch-listener").Logger()

	ctx, cancel := context.WithCancel(context.Background())

	listener := &L1Listener{
		client:       client,
		beaconAPI:    beaconAPI,
		log:          logger,
		batchFactor:  cfg.BatchFactor,
		pollInterval: cfg.PollInterval,
		epochCh:      make(chan EpochEvent, 10),
		triggerCh:    make(chan BatchTrigger, 10),
		errorCh:      make(chan error, 10),
		ctx:          ctx,
		cancel:       cancel,
	}

	if listener.batchFactor == 0 {
		listener.batchFactor = 10
	}
	if listener.pollInterval == 0 {
		listener.pollInterval = 12 * time.Second
	}

	logger.Info().
		Str("l1_rpc", cfg.L1RPC).
		Str("beacon_rpc", cfg.BeaconRPC).
		Uint64("batch_factor", listener.batchFactor).
		Dur("poll_interval", listener.pollInterval).
		Msg("L1 batch listener initialized")

	return listener, nil
}

// Start begins listening for L1 events
func (l *L1Listener) Start(ctx context.Context) error {
	l.log.Info().Msg("Starting L1 batch listener")

	// Get current epoch to establish baseline
	currentEpoch, err := l.getCurrentEpoch(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current epoch: %w", err)
	}

	l.lastProcessedEpoch = currentEpoch
	l.log.Info().Uint64("starting_epoch", currentEpoch).Msg("L1 listener established baseline")

	go l.pollLoop(ctx)

	return nil
}

// Stop gracefully stops the listener
func (l *L1Listener) Stop(ctx context.Context) error {
	l.log.Info().Msg("Stopping L1 batch listener")
	l.cancel()

	if l.client != nil {
		l.client.Close()
	}

	// Close channels
	close(l.epochCh)
	close(l.triggerCh)
	close(l.errorCh)

	return nil
}

// EpochEvents returns the channel for epoch events
func (l *L1Listener) EpochEvents() <-chan EpochEvent {
	return l.epochCh
}

// BatchTriggers returns the channel for batch triggers
func (l *L1Listener) BatchTriggers() <-chan BatchTrigger {
	return l.triggerCh
}

// Errors returns the channel for errors
func (l *L1Listener) Errors() <-chan error {
	return l.errorCh
}

// pollLoop continuously polls L1 for new epochs
func (l *L1Listener) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(l.pollInterval)
	defer ticker.Stop()

	l.log.Info().Msg("L1 poll loop started")

	for {
		select {
		case <-ctx.Done():
			l.log.Info().Msg("L1 poll loop stopping")
			return
		case <-l.ctx.Done():
			l.log.Info().Msg("L1 poll loop canceled")
			return
		case <-ticker.C:
			if err := l.checkForNewEpochs(ctx); err != nil {
				l.log.Error().Err(err).Msg("Failed to check for new epochs")
				select {
				case l.errorCh <- err:
				default:
					l.log.Warn().Msg("Error channel full, dropping error")
				}
			}
		}
	}
}

// checkForNewEpochs checks if we've entered a new epoch and processes it
func (l *L1Listener) checkForNewEpochs(ctx context.Context) error {
	currentEpoch, err := l.getCurrentEpoch(ctx)
	if err != nil {
		return fmt.Errorf("get current epoch: %w", err)
	}

	// Process any missed epochs
	for epoch := l.lastProcessedEpoch + 1; epoch <= currentEpoch; epoch++ {
		if err := l.processEpoch(ctx, epoch); err != nil {
			l.log.Error().Uint64("epoch", epoch).Err(err).Msg("Failed to process epoch")
			return err
		}
	}

	l.lastProcessedEpoch = currentEpoch
	return nil
}

// processEpoch handles a single epoch and determines if it should trigger batch events
func (l *L1Listener) processEpoch(ctx context.Context, epoch uint64) error {
	l.log.Debug().Uint64("epoch", epoch).Msg("Processing epoch")

	// Get epoch details
	header, err := l.getEpochHeader(ctx, epoch)
	if err != nil {
		return fmt.Errorf("get epoch header: %w", err)
	}

	epochEvent := EpochEvent{
		EpochNumber: epoch,
		BlockNumber: header.Number.Uint64(),
		BlockHash:   header.Hash(),
		Timestamp:   time.Unix(int64(header.Time), 0),
	}

	// Send epoch event
	select {
	case l.epochCh <- epochEvent:
	default:
		l.log.Warn().Uint64("epoch", epoch).Msg("Epoch channel full, dropping event")
	}

	// Check if this epoch triggers a batch event
	if epoch%l.batchFactor == 0 {
		trigger := BatchTrigger{
			TriggerEpoch: epoch,
			TriggerTime:  epochEvent.Timestamp,
			ShouldStart:  true, // New batch starts
		}

		l.log.Info().
			Uint64("epoch", epoch).
			Uint64("batch_factor", l.batchFactor).
			Time("trigger_time", trigger.TriggerTime).
			Msg("Batch trigger detected")

		select {
		case l.triggerCh <- trigger:
		default:
			l.log.Warn().Uint64("epoch", epoch).Msg("Trigger channel full, dropping trigger")
		}
	}

	return nil
}

// getCurrentEpoch gets the current Ethereum epoch from beacon chain API
func (l *L1Listener) getCurrentEpoch(ctx context.Context) (uint64, error) {
	data, err := l.beaconAPI.GetCurrentEpoch(ctx)
	if err != nil {
		return 0, fmt.Errorf("get current epoch from beacon API: %w", err)
	}
	return data.Epoch, nil
}

// getEpochHeader gets the header for the first block of an epoch
func (l *L1Listener) getEpochHeader(ctx context.Context, epoch uint64) (*types.Header, error) {
	data, err := l.beaconAPI.GetEpochData(ctx, epoch)
	if err != nil {
		return nil, fmt.Errorf("get epoch data from beacon API: %w", err)
	}

	header, err := l.client.HeaderByNumber(ctx, big.NewInt(int64(data.BlockNumber)))
	if err != nil {
		return nil, fmt.Errorf("get header for block %d: %w", data.BlockNumber, err)
	}

	return header, nil
}

// GetCurrentBatchNumber returns the current batch number based on epochs
func (l *L1Listener) GetCurrentBatchNumber(ctx context.Context) (uint64, error) {
	epoch, err := l.getCurrentEpoch(ctx)
	if err != nil {
		return 0, err
	}

	return epoch / l.batchFactor, nil
}

// IsNewBatchEpoch checks if the given epoch should trigger a new batch
func (l *L1Listener) IsNewBatchEpoch(epoch uint64) bool {
	return epoch%l.batchFactor == 0
}
