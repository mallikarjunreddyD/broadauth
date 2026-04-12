package slot

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// AdaptiveSlotSource implements the SlotSource interface but allows dynamic scaling of T_i.
type AdaptiveSlotSource struct {
	currentSlot uint64
	currentT    uint64 // duration in milliseconds
	mu          sync.RWMutex
}

func NewAdaptiveSlotSource(initialTMs uint64) *AdaptiveSlotSource {
	return &AdaptiveSlotSource{
		currentSlot: 1, // Start at slot 1
		currentT:    initialTMs,
	}
}

// GetSlot returns the current dynamically scaled slot
func (a *AdaptiveSlotSource) GetSlot() (Slot, error) {
	return Slot(atomic.LoadUint64(&a.currentSlot)), nil
}

// SetDuration allows the RCD to dynamically change T_i based on network congestion
func (a *AdaptiveSlotSource) SetDuration(tMs uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.currentT = tMs
}

// Ticker yields a channel that ticks at the dynamic rate T_i
func (a *AdaptiveSlotSource) Ticker(ctx context.Context) <-chan Slot {
	ch := make(chan Slot)

	go func() {
		defer close(ch)
		for {
			// Fetch the current duration safely
			a.mu.RLock()
			d := time.Duration(a.currentT) * time.Millisecond
			a.mu.RUnlock()

			timer := time.NewTimer(d)

			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				// Advance the slot
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
