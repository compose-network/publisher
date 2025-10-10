package slot

import (
	"context"
	"sync"
	"time"
)

// slotImpl is the concrete implementation of the Slot interface.
// It handles 12-second Ethereum-aligned slots for SBCP coordination.
type slotImpl struct {
	mu                  sync.RWMutex
	genesisTime         time.Time
	slotDuration        time.Duration
	sealCutoverFraction float64
}

// New creates a new Slot implementation.
func New(genesisTime time.Time, slotDuration time.Duration, sealCutoverFraction float64) Slot {
	return &slotImpl{
		genesisTime:         genesisTime,
		slotDuration:        slotDuration,
		sealCutoverFraction: sealCutoverFraction,
	}
}

func (s *slotImpl) GetCurrent() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if time.Now().Before(s.genesisTime) {
		return 0
	}

	elapsed := time.Since(s.genesisTime)
	slot := uint64(elapsed/s.slotDuration) + 1
	return slot
}

func (s *slotImpl) GetStartTime(slot uint64) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if slot == 0 {
		return s.genesisTime
	}

	duration := time.Duration(slot-1) * s.slotDuration
	return s.genesisTime.Add(duration)
}

func (s *slotImpl) GetProgress() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	currentSlot := s.getCurrentUnlocked()
	slotStart := s.getStartTimeUnlocked(currentSlot)

	elapsed := time.Since(slotStart)
	progress := float64(elapsed) / float64(s.slotDuration)

	if progress < 0 {
		return 0
	}
	if progress > 1 {
		return 1
	}
	return progress
}

func (s *slotImpl) IsSealTime() bool {
	return s.GetProgress() >= s.sealCutoverFraction
}

func (s *slotImpl) WaitForNext(ctx context.Context) error {
	currentSlot := s.GetCurrent()
	nextSlotStart := s.GetStartTime(currentSlot + 1)

	timer := time.NewTimer(time.Until(nextSlotStart))
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *slotImpl) SetGenesisTime(genesis time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.genesisTime = genesis
}

func (s *slotImpl) GetSealTime(slot uint64) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	slotStart := s.getStartTimeUnlocked(slot)
	sealOffset := time.Duration(float64(s.slotDuration) * s.sealCutoverFraction)
	return slotStart.Add(sealOffset)
}

func (s *slotImpl) GetEndTime(slot uint64) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	slotStart := s.getStartTimeUnlocked(slot)
	return slotStart.Add(s.slotDuration)
}

func (s *slotImpl) TimeUntilSeal() time.Duration {
	currentSlot := s.GetCurrent()
	sealTime := s.GetSealTime(currentSlot)
	return time.Until(sealTime)
}

func (s *slotImpl) TimeUntilEnd() time.Duration {
	currentSlot := s.GetCurrent()
	endTime := s.GetEndTime(currentSlot)
	return time.Until(endTime)
}

func (s *slotImpl) getCurrentUnlocked() uint64 {
	if time.Now().Before(s.genesisTime) {
		return 0
	}

	elapsed := time.Since(s.genesisTime)
	slot := uint64(elapsed/s.slotDuration) + 1
	return slot
}

func (s *slotImpl) getStartTimeUnlocked(slot uint64) time.Time {
	if slot == 0 {
		return s.genesisTime
	}

	duration := time.Duration(slot-1) * s.slotDuration
	return s.genesisTime.Add(duration)
}
