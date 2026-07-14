package slot

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// AdaptiveSlotSource is a SlotSource whose tick interval can be changed at
// runtime by an external controller (see rcd.selectNextDuration). Unlike
// BeaconChainSlotSource, it is not anchored to any shared/external clock:
// each process using one advances its own slot counter independently, at
// whatever duration SetDuration last configured. Callers that need to make
// safety decisions across processes (e.g. a receiver deciding whether to
// trust a disclosed key) must not compare slot numbers from two different
// AdaptiveSlotSource instances against each other; they must use wall-clock
// time instead.
type AdaptiveSlotSource struct {
	currentSlot uint64

	mu           sync.RWMutex
	currentDurMs uint64 // duration in milliseconds
}

// NewAdaptiveSlotSource creates a source starting at slot 1, ticking at
// initialDurationMs milliseconds until SetDuration changes it.
func NewAdaptiveSlotSource(initialDurationMs uint64) *AdaptiveSlotSource {
	return &AdaptiveSlotSource{
		currentSlot:  1,
		currentDurMs: initialDurationMs,
	}
}

// GetSlot returns the current slot number.
func (a *AdaptiveSlotSource) GetSlot() (Slot, error) {
	return Slot(atomic.LoadUint64(&a.currentSlot)), nil
}

// SetDuration updates the tick interval used for all future ticks. Takes
// effect the next time the running timer re-arms (i.e. from the next tick
// onward, not retroactively on the in-flight one).
func (a *AdaptiveSlotSource) SetDuration(ms uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.currentDurMs = ms
}

// GetDuration returns the tick interval currently in effect.
func (a *AdaptiveSlotSource) GetDuration() uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.currentDurMs
}

// Ticker yields a channel that ticks at the current (possibly changing)
// duration, advancing the slot counter by 1 on every tick.
func (a *AdaptiveSlotSource) Ticker(ctx context.Context) <-chan Slot {
	ch := make(chan Slot)

	go func() {
		defer close(ch)
		for {
			timer := time.NewTimer(time.Duration(a.GetDuration()) * time.Millisecond)

			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				newSlot := atomic.AddUint64(&a.currentSlot, 1)
				select {
				case ch <- Slot(newSlot):
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch
}
