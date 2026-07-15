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

	startOnce sync.Once
	subsMu    sync.Mutex
	subs      map[chan Slot]struct{}
}

// NewAdaptiveSlotSource creates a source starting at slot 1, ticking at
// initialDurationMs milliseconds until SetDuration changes it.
func NewAdaptiveSlotSource(initialDurationMs uint64) *AdaptiveSlotSource {
	return &AdaptiveSlotSource{
		currentSlot:  1,
		currentDurMs: initialDurationMs,
		subs:         make(map[chan Slot]struct{}),
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
//
// Safe to call more than once on the same instance (rcd.go's broadcastLoop
// and disclosureWorker both need their own slot ticks): the counter is only
// ever advanced by a single background loop, started lazily on the first
// call and fanned out to every subscriber's channel. A second independent
// increment loop per call would race the counter forward at N times the
// intended rate - this previously caused disclosure-delay pacing to blow
// through its own target slots almost instantly (only caught by the
// receiver's separate wall-clock Theorem-1 check, not by this pacing
// itself).
//
// The channel is buffered by 1 and the fan-out send is non-blocking: if a
// subscriber (e.g. disclosureWorker mid-Broadcast on a burst of ready
// disclosures) hasn't consumed the previous tick yet, this tick is dropped
// for it rather than blocking. That's deliberate - GetSlot()'s atomic
// counter is the real source of truth and always advances on schedule; a
// channel send here is only a wake-up nudge. A *blocking* send would let
// one slow subscriber stall the shared counter for every other subscriber
// and every GetSlot() caller (this was a real bug: a busy disclosureWorker
// once froze the counter at slot 1 for 20+ real seconds, causing a
// hashchain-refill request to read a stale slot value long after real time
// had moved on).
//
// The returned channel is never closed (only ctx.Done() signals the end -
// every caller in this codebase already selects on that directly rather
// than relying on the channel closing).
func (a *AdaptiveSlotSource) Ticker(ctx context.Context) <-chan Slot {
	ch := make(chan Slot, 1)

	a.subsMu.Lock()
	a.subs[ch] = struct{}{}
	a.subsMu.Unlock()

	a.startOnce.Do(func() { go a.run(ctx) })

	return ch
}

func (a *AdaptiveSlotSource) run(ctx context.Context) {
	for {
		timer := time.NewTimer(time.Duration(a.GetDuration()) * time.Millisecond)

		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			newSlot := atomic.AddUint64(&a.currentSlot, 1)

			a.subsMu.Lock()
			subs := make([]chan Slot, 0, len(a.subs))
			for ch := range a.subs {
				subs = append(subs, ch)
			}
			a.subsMu.Unlock()

			for _, ch := range subs {
				select {
				case ch <- Slot(newSlot):
				default:
				}
			}
		}
	}
}
