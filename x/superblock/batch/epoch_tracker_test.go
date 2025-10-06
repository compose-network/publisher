package batch

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEpochTrackerCreation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  EpochTrackerConfig
		wantErr bool
	}{
		{
			name: "valid config with ethereum mainnet genesis",
			config: EpochTrackerConfig{
				GenesisTime: EthereumMainnetGenesis,
				BatchFactor: BatchFactor,
			},
			wantErr: false,
		},
		{
			name: "valid config with custom genesis",
			config: EpochTrackerConfig{
				GenesisTime: time.Now().Add(-24 * time.Hour).Unix(),
				BatchFactor: 5,
			},
			wantErr: false,
		},
		{
			name: "invalid config - zero genesis time",
			config: EpochTrackerConfig{
				GenesisTime: 0,
				BatchFactor: 10,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := zerolog.Nop()
			tracker, err := NewEpochTracker(tt.config, log)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, tracker)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, tracker)
			}
		})
	}
}

func TestEpochCalculation(t *testing.T) {
	t.Parallel()

	// Use Ethereum Mainnet genesis
	genesisTime := time.Unix(EthereumMainnetGenesis, 0).UTC()

	tests := []struct {
		name          string
		currentTime   time.Time
		expectedEpoch uint64
		expectedSlot  uint64
	}{
		{
			name:          "at genesis",
			currentTime:   genesisTime,
			expectedEpoch: 0,
			expectedSlot:  0,
		},
		{
			name:          "one slot after genesis",
			currentTime:   genesisTime.Add(12 * time.Second),
			expectedEpoch: 0,
			expectedSlot:  1,
		},
		{
			name:          "one epoch after genesis (32 slots)",
			currentTime:   genesisTime.Add(32 * 12 * time.Second), // 384 seconds
			expectedEpoch: 1,
			expectedSlot:  32,
		},
		{
			name:          "10 epochs after genesis",
			currentTime:   genesisTime.Add(10 * 32 * 12 * time.Second),
			expectedEpoch: 10,
			expectedSlot:  320,
		},
		{
			name:          "before genesis",
			currentTime:   genesisTime.Add(-1 * time.Hour),
			expectedEpoch: 0,
			expectedSlot:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := zerolog.Nop()
			tracker, err := NewEpochTracker(EpochTrackerConfig{
				GenesisTime: genesisTime.Unix(),
				BatchFactor: 10,
			}, log)
			require.NoError(t, err)

			epoch := tracker.getEpochFromTime(tt.currentTime)
			slot := tracker.getSlotFromTime(tt.currentTime)

			assert.Equal(t, tt.expectedEpoch, epoch, "epoch mismatch")
			assert.Equal(t, tt.expectedSlot, slot, "slot mismatch")
		})
	}
}

func TestBatchTriggerDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		epoch         uint64
		batchFactor   uint64
		shouldTrigger bool
	}{
		{
			name:          "epoch 0 triggers batch (factor 10)",
			epoch:         0,
			batchFactor:   10,
			shouldTrigger: true,
		},
		{
			name:          "epoch 10 triggers batch (factor 10)",
			epoch:         10,
			batchFactor:   10,
			shouldTrigger: true,
		},
		{
			name:          "epoch 20 triggers batch (factor 10)",
			epoch:         20,
			batchFactor:   10,
			shouldTrigger: true,
		},
		{
			name:          "epoch 5 does not trigger batch (factor 10)",
			epoch:         5,
			batchFactor:   10,
			shouldTrigger: false,
		},
		{
			name:          "epoch 15 does not trigger batch (factor 10)",
			epoch:         15,
			batchFactor:   10,
			shouldTrigger: false,
		},
		{
			name:          "epoch 4 triggers batch (factor 2)",
			epoch:         4,
			batchFactor:   2,
			shouldTrigger: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := zerolog.Nop()
			tracker, err := NewEpochTracker(EpochTrackerConfig{
				GenesisTime: EthereumMainnetGenesis,
				BatchFactor: tt.batchFactor,
			}, log)
			require.NoError(t, err)

			shouldTrigger := tracker.IsNewBatchEpoch(tt.epoch)
			assert.Equal(t, tt.shouldTrigger, shouldTrigger)
		})
	}
}

func TestGetCurrentBatchNumber(t *testing.T) {
	t.Parallel()

	genesisTime := time.Unix(EthereumMainnetGenesis, 0).UTC()
	batchFactor := uint64(BatchFactor)

	tests := []struct {
		name          string
		currentTime   time.Time
		expectedBatch uint64
	}{
		{
			name:          "batch 0 - at genesis",
			currentTime:   genesisTime,
			expectedBatch: 0,
		},
		{
			name:          "batch 0 - epoch 5",
			currentTime:   genesisTime.Add(5 * 32 * 12 * time.Second),
			expectedBatch: 0,
		},
		{
			name:          "batch 1 - epoch 10",
			currentTime:   genesisTime.Add(10 * 32 * 12 * time.Second),
			expectedBatch: 1,
		},
		{
			name:          "batch 2 - epoch 20",
			currentTime:   genesisTime.Add(20 * 32 * 12 * time.Second),
			expectedBatch: 2,
		},
		{
			name:          "batch 10 - epoch 100",
			currentTime:   genesisTime.Add(100 * 32 * 12 * time.Second),
			expectedBatch: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := zerolog.Nop()
			tracker, err := NewEpochTracker(EpochTrackerConfig{
				GenesisTime: genesisTime.Unix(),
				BatchFactor: batchFactor,
			}, log)
			require.NoError(t, err)

			epoch := tracker.getEpochFromTime(tt.currentTime)
			batchNumber := epoch / batchFactor

			assert.Equal(t, tt.expectedBatch, batchNumber)
		})
	}
}

func TestEpochTrackerStartStop(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()
	tracker, err := NewEpochTracker(EpochTrackerConfig{
		GenesisTime: EthereumMainnetGenesis,
		BatchFactor: BatchFactor,
	}, log)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start tracker
	err = tracker.Start(ctx)
	require.NoError(t, err)

	// Let it run for a bit
	time.Sleep(200 * time.Millisecond)

	// Stop tracker
	err = tracker.Stop(ctx)
	require.NoError(t, err)
}

func TestEpochTrackerStats(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()
	tracker, err := NewEpochTracker(EpochTrackerConfig{
		GenesisTime: EthereumMainnetGenesis,
		BatchFactor: BatchFactor,
	}, log)
	require.NoError(t, err)

	stats := tracker.GetStats()

	// Verify stats structure
	assert.Contains(t, stats, "current_epoch")
	assert.Contains(t, stats, "current_slot")
	assert.Contains(t, stats, "current_batch_number")
	assert.Contains(t, stats, "batch_factor")
	assert.Contains(t, stats, "genesis_time")
	assert.Contains(t, stats, "next_batch_epoch")

	// Verify types
	assert.IsType(t, uint64(0), stats["current_epoch"])
	assert.IsType(t, uint64(0), stats["current_slot"])
	assert.IsType(t, uint64(0), stats["current_batch_number"])
}

func TestGetEpochStartTime(t *testing.T) {
	t.Parallel()

	genesisTime := time.Unix(EthereumMainnetGenesis, 0).UTC()
	log := zerolog.Nop()

	tracker, err := NewEpochTracker(EpochTrackerConfig{
		GenesisTime: genesisTime.Unix(),
		BatchFactor: BatchFactor,
	}, log)
	require.NoError(t, err)

	tests := []struct {
		name         string
		epoch        uint64
		expectedTime time.Time
	}{
		{
			name:         "epoch 0",
			epoch:        0,
			expectedTime: genesisTime,
		},
		{
			name:         "epoch 1",
			epoch:        1,
			expectedTime: genesisTime.Add(1 * SecondsPerEpoch * time.Second),
		},
		{
			name:         "epoch 10",
			epoch:        10,
			expectedTime: genesisTime.Add(10 * SecondsPerEpoch * time.Second),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			startTime := tracker.GetEpochStartTime(tt.epoch)
			assert.Equal(t, tt.expectedTime.Unix(), startTime.Unix())
		})
	}
}

func TestGetSlotStartTime(t *testing.T) {
	t.Parallel()

	genesisTime := time.Unix(EthereumMainnetGenesis, 0).UTC()
	log := zerolog.Nop()

	tracker, err := NewEpochTracker(EpochTrackerConfig{
		GenesisTime: genesisTime.Unix(),
		BatchFactor: BatchFactor,
	}, log)
	require.NoError(t, err)

	tests := []struct {
		name         string
		slot         uint64
		expectedTime time.Time
	}{
		{
			name:         "slot 0",
			slot:         0,
			expectedTime: genesisTime,
		},
		{
			name:         "slot 1",
			slot:         1,
			expectedTime: genesisTime.Add(1 * SlotDuration),
		},
		{
			name:         "slot 32 (first slot of epoch 1)",
			slot:         32,
			expectedTime: genesisTime.Add(32 * SlotDuration),
		},
		{
			name:         "slot 320 (first slot of epoch 10)",
			slot:         320,
			expectedTime: genesisTime.Add(320 * SlotDuration),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			startTime := tracker.GetSlotStartTime(tt.slot)
			assert.Equal(t, tt.expectedTime.Unix(), startTime.Unix())
		})
	}
}

func TestDefaultEpochTrackerConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultEpochTrackerConfig()

	// Verify Ethereum Mainnet genesis time
	assert.Equal(t, int64(EthereumMainnetGenesis), cfg.GenesisTime)

	// Verify batch factor is 10 (spec requirement)
	assert.Equal(t, uint64(BatchFactor), cfg.BatchFactor)
}

// TestEpochSynchronization verifies that two epoch trackers with the same genesis time
// will always calculate the same epoch and batch numbers at the same time
func TestEpochSynchronization(t *testing.T) {
	t.Parallel()

	genesisTime := time.Unix(EthereumMainnetGenesis, 0).UTC()
	log := zerolog.Nop()

	// Create two trackers with identical configuration
	tracker1, err := NewEpochTracker(EpochTrackerConfig{
		GenesisTime: genesisTime.Unix(),
		BatchFactor: BatchFactor,
	}, log)
	require.NoError(t, err)

	tracker2, err := NewEpochTracker(EpochTrackerConfig{
		GenesisTime: genesisTime.Unix(),
		BatchFactor: BatchFactor,
	}, log)
	require.NoError(t, err)

	// Test at various points in time
	testTimes := []time.Time{
		genesisTime,
		genesisTime.Add(100 * 32 * 12 * time.Second),
		genesisTime.Add(500 * 32 * 12 * time.Second),
		time.Now(),
	}

	for _, testTime := range testTimes {
		epoch1 := tracker1.getEpochFromTime(testTime)
		epoch2 := tracker2.getEpochFromTime(testTime)
		assert.Equal(t, epoch1, epoch2, "epochs should match at time %v", testTime)

		slot1 := tracker1.getSlotFromTime(testTime)
		slot2 := tracker2.getSlotFromTime(testTime)
		assert.Equal(t, slot1, slot2, "slots should match at time %v", testTime)

		batch1 := epoch1 / tracker1.batchFactor
		batch2 := epoch2 / tracker2.batchFactor
		assert.Equal(t, batch1, batch2, "batch numbers should match at time %v", testTime)
	}
}
