package batch

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	SlotDuration    = 12 * time.Second // Ethereum slot time
	SlotsPerEpoch   = 32               // Ethereum consensus spec
	SecondsPerSlot  = 12
	SecondsPerEpoch = SecondsPerSlot * SlotsPerEpoch // 384 seconds
)

// EpochEvent represents an Ethereum epoch transition event
type EpochEvent struct {
	EpochNumber uint64
	Slot        uint64
	Timestamp   time.Time
}

// BatchTrigger represents a batch synchronization trigger
type BatchTrigger struct {
	TriggerEpoch uint64
	TriggerSlot  uint64
	TriggerTime  time.Time
}

// EpochTracker monitors time-based Ethereum epochs and triggers batch events
type EpochTracker struct {
	mu                 sync.RWMutex
	log                zerolog.Logger
	genesisTime        time.Time
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

// EpochTrackerConfig holds configuration for the epoch tracker
// //nolint: lll // Config struct is long
type EpochTrackerConfig struct {
	GenesisTime  time.Time     `mapstructure:"genesis_time"  yaml:"genesis_time"`  // Common genesis time for all sequencers
	BatchFactor  uint64        `mapstructure:"batch_factor"  yaml:"batch_factor"`  // Trigger batch every N epochs (default: 10)
	PollInterval time.Duration `mapstructure:"poll_interval" yaml:"poll_interval"` // How often to check for epoch changes
}

// DefaultEpochTrackerConfig returns sensible defaults
func DefaultEpochTrackerConfig() EpochTrackerConfig {
	// Ethereum Mainnet genesis: 2020-12-01 12:00:23 UTC
	ethereumGenesisTime := time.Unix(1606824023, 0).UTC()

	return EpochTrackerConfig{
		GenesisTime:  ethereumGenesisTime,
		BatchFactor:  10,
		PollInterval: 12 * time.Second, // Ethereum slot time
	}
}

// NewEpochTracker creates a new epoch tracker with time-based calculation
func NewEpochTracker(cfg EpochTrackerConfig, log zerolog.Logger) (*EpochTracker, error) {
	if cfg.GenesisTime.IsZero() {
		return nil, fmt.Errorf("genesis_time is required")
	}

	logger := log.With().Str("component", "epoch-tracker").Logger()

	ctx, cancel := context.WithCancel(context.Background())

	batchFactor := cfg.BatchFactor
	if batchFactor == 0 {
		batchFactor = 10
	}

	pollInterval := cfg.PollInterval
	if pollInterval == 0 {
		pollInterval = 12 * time.Second
	}

	tracker := &EpochTracker{
		log:          logger,
		genesisTime:  cfg.GenesisTime,
		batchFactor:  batchFactor,
		pollInterval: pollInterval,
		epochCh:      make(chan EpochEvent, 10),
		triggerCh:    make(chan BatchTrigger, 10),
		errorCh:      make(chan error, 10),
		ctx:          ctx,
		cancel:       cancel,
	}

	logger.Info().
		Time("genesis_time", tracker.genesisTime).
		Uint64("batch_factor", tracker.batchFactor).
		Dur("poll_interval", tracker.pollInterval).
		Msg("Epoch tracker initialized (time-based calculation)")

	return tracker, nil
}

// Start begins monitoring epochs
func (t *EpochTracker) Start(ctx context.Context) error {
	t.log.Info().Msg("Starting epoch tracker")

	// Get current epoch to establish baseline
	currentEpoch := t.GetCurrentEpoch()
	currentSlot := t.GetCurrentSlot()

	t.mu.Lock()
	t.lastProcessedEpoch = currentEpoch
	t.mu.Unlock()

	t.log.Info().
		Uint64("starting_epoch", currentEpoch).
		Uint64("starting_slot", currentSlot).
		Msg("Epoch tracker baseline established")

	go t.pollLoop(ctx)

	return nil
}

// Stop gracefully stops the tracker
func (t *EpochTracker) Stop(ctx context.Context) error {
	t.log.Info().Msg("Stopping epoch tracker")
	t.cancel()

	// Close channels
	close(t.epochCh)
	close(t.triggerCh)
	close(t.errorCh)

	return nil
}

// GetCurrentEpoch calculates the current Ethereum epoch from time
// Formula: epoch = (time.Now() - genesis) / 12 seconds / 32 slots
func (t *EpochTracker) GetCurrentEpoch() uint64 {
	return t.getEpochFromTime(time.Now())
}

// GetCurrentSlot calculates the current Ethereum slot from time
// Formula: slot = (time.Now() - genesis) / 12 seconds
func (t *EpochTracker) GetCurrentSlot() uint64 {
	return t.getSlotFromTime(time.Now())
}

// GetCurrentBatchNumber returns the current batch number
func (t *EpochTracker) GetCurrentBatchNumber() uint64 {
	epoch := t.GetCurrentEpoch()
	return epoch / t.batchFactor
}

// IsNewBatchEpoch checks if the given epoch triggers a new batch
func (t *EpochTracker) IsNewBatchEpoch(epoch uint64) bool {
	return epoch%t.batchFactor == 0
}

// GetEpochStartTime returns the start time of a given epoch
func (t *EpochTracker) GetEpochStartTime(epoch uint64) time.Time {
	epochDuration := time.Duration(epoch) * SecondsPerEpoch * time.Second
	return t.genesisTime.Add(epochDuration)
}

// GetSlotStartTime returns the start time of a given slot
func (t *EpochTracker) GetSlotStartTime(slot uint64) time.Time {
	slotDuration := time.Duration(slot) * SlotDuration
	return t.genesisTime.Add(slotDuration)
}

// EpochEvents returns the channel for epoch events
func (t *EpochTracker) EpochEvents() <-chan EpochEvent {
	return t.epochCh
}

// BatchTriggers returns the channel for batch triggers
func (t *EpochTracker) BatchTriggers() <-chan BatchTrigger {
	return t.triggerCh
}

// Errors returns the channel for errors
func (t *EpochTracker) Errors() <-chan error {
	return t.errorCh
}

// pollLoop continuously checks for epoch changes
func (t *EpochTracker) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(t.pollInterval)
	defer ticker.Stop()

	t.log.Info().Msg("Epoch poll loop started")

	for {
		select {
		case <-ctx.Done():
			t.log.Info().Msg("Epoch poll loop stopping (context done)")
			return
		case <-t.ctx.Done():
			t.log.Info().Msg("Epoch poll loop stopping (tracker canceled)")
			return
		case <-ticker.C:
			if err := t.checkForNewEpochs(); err != nil {
				t.log.Error().Err(err).Msg("Failed to check for new epochs")
				select {
				case t.errorCh <- err:
				default:
					t.log.Warn().Msg("Error channel full, dropping error")
				}
			}
		}
	}
}

// checkForNewEpochs checks if we've entered a new epoch and processes it
func (t *EpochTracker) checkForNewEpochs() error {
	currentEpoch := t.GetCurrentEpoch()

	t.mu.Lock()
	lastEpoch := t.lastProcessedEpoch
	t.mu.Unlock()

	// Process any missed epochs
	for epoch := lastEpoch + 1; epoch <= currentEpoch; epoch++ {
		if err := t.processEpoch(epoch); err != nil {
			t.log.Error().Uint64("epoch", epoch).Err(err).Msg("Failed to process epoch")
			return err
		}
	}

	t.mu.Lock()
	t.lastProcessedEpoch = currentEpoch
	t.mu.Unlock()

	return nil
}

// processEpoch handles a single epoch and determines if it should trigger batch events
func (t *EpochTracker) processEpoch(epoch uint64) error {
	t.log.Debug().Uint64("epoch", epoch).Msg("Processing epoch")

	epochStartTime := t.GetEpochStartTime(epoch)
	firstSlot := epoch * SlotsPerEpoch

	epochEvent := EpochEvent{
		EpochNumber: epoch,
		Slot:        firstSlot,
		Timestamp:   epochStartTime,
	}

	// Send epoch event
	select {
	case t.epochCh <- epochEvent:
		t.log.Debug().Uint64("epoch", epoch).Msg("Epoch event sent")
	default:
		t.log.Warn().Uint64("epoch", epoch).Msg("Epoch channel full, dropping event")
	}

	// Check if this epoch triggers a batch event
	if t.IsNewBatchEpoch(epoch) {
		trigger := BatchTrigger{
			TriggerEpoch: epoch,
			TriggerSlot:  firstSlot,
			TriggerTime:  epochStartTime,
		}

		t.log.Info().
			Uint64("epoch", epoch).
			Uint64("slot", firstSlot).
			Uint64("batch_factor", t.batchFactor).
			Uint64("batch_number", epoch/t.batchFactor).
			Time("trigger_time", trigger.TriggerTime).
			Msg("Batch trigger detected")

		select {
		case t.triggerCh <- trigger:
			t.log.Info().Uint64("epoch", epoch).Msg("Batch trigger sent")
		default:
			t.log.Warn().Uint64("epoch", epoch).Msg("Trigger channel full, dropping trigger")
		}
	}

	return nil
}

// getEpochFromTime calculates epoch from a specific time
func (t *EpochTracker) getEpochFromTime(ts time.Time) uint64 {
	if ts.Before(t.genesisTime) {
		return 0
	}

	elapsed := ts.Sub(t.genesisTime).Seconds()
	slot := uint64(elapsed / SecondsPerSlot)
	epoch := slot / SlotsPerEpoch

	return epoch
}

// getSlotFromTime calculates slot from a specific time
func (t *EpochTracker) getSlotFromTime(ts time.Time) uint64 {
	if ts.Before(t.genesisTime) {
		return 0
	}

	elapsed := ts.Sub(t.genesisTime).Seconds()
	slot := uint64(elapsed / SecondsPerSlot)

	return slot
}

// GetStats returns current statistics
func (t *EpochTracker) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	currentEpoch := t.GetCurrentEpoch()
	currentSlot := t.GetCurrentSlot()
	batchNumber := t.GetCurrentBatchNumber()

	return map[string]interface{}{
		"current_epoch":        currentEpoch,
		"current_slot":         currentSlot,
		"current_batch_number": batchNumber,
		"last_processed_epoch": t.lastProcessedEpoch,
		"batch_factor":         t.batchFactor,
		"genesis_time":         t.genesisTime.Format(time.RFC3339),
		"next_batch_epoch":     ((currentEpoch / t.batchFactor) + 1) * t.batchFactor,
	}
}
