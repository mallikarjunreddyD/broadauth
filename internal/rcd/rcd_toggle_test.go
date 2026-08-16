package rcd

import (
	"math"
	"testing"
)

// This file proves the adaptive slot-duration toggle is *measurable* and
// *responds to the condition at the disclosure queue*, without needing anvil,
// a UDP radio, or Linux `tc` shaping. It closes the loop the real system runs:
//
//   producer:   +1 disclosure per slot (one Bloom-filter flush per slot)
//   radio:      drains radioBytesPerSec of disclosures during the slot's
//               wall-clock duration T_cur (longer slot => more bytes drained,
//               the "cryptographic amortization" from GUIDE.md 2.1)
//   controller: reads the backlog it observes and re-arms T_cur via the real
//               production selectNextDuration()
//
// Fixed point (drain == production): radioBps * T/1000 / msgSize == 1, i.e.
//   T* = 1000 * msgSize / radioBps  ms
// so a tighter radio settles at a LONGER slot -- exactly the toggle the paper
// claims and the flat charts failed to show.

// simulateAdaptiveLoop runs the closed loop for `slots` slots and returns the
// per-slot T_cur trajectory and the observed backlog trajectory. The physics
// (backlog drain) is continuous; the controller sees the rounded integer queue
// depth, exactly like the atomic gauge adjustSlotDuration reads in production.
func simulateAdaptiveLoop(radioBps, msgSize, slots int, tMin, tMax uint64) (tHist []uint64, backlogHist []int) {
	backlog := 0.0
	t := tMin
	tHist = make([]uint64, slots)
	backlogHist = make([]int, slots)

	for i := 0; i < slots; i++ {
		// One disclosure produced this slot.
		backlog += 1.0
		// Radio drains during this slot's real duration T (ms).
		capacity := float64(radioBps) * (float64(t) / 1000.0) / float64(msgSize)
		if capacity > backlog {
			capacity = backlog
		}
		backlog -= capacity

		qlen := int(math.Round(backlog))
		if qlen < 0 {
			qlen = 0
		}
		// The real controller, unchanged.
		t = selectNextDuration(qlen, adaptiveQueueCap, t, tMin, tMax)

		tHist[i] = t
		backlogHist[i] = qlen
	}
	return tHist, backlogHist
}

func settledMean(hist []uint64) float64 {
	// Average the last quarter of the run (post-transient).
	start := len(hist) * 3 / 4
	var sum float64
	for _, v := range hist[start:] {
		sum += float64(v)
	}
	return sum / float64(len(hist)-start)
}

func maxU64(hist []uint64) uint64 {
	var m uint64
	for _, v := range hist {
		if v > m {
			m = v
		}
	}
	return m
}

func maxInt(hist []int) int {
	m := 0
	for _, v := range hist {
		if v > m {
			m = v
		}
	}
	return m
}

const (
	simTMin    = uint64(1000)
	simTMax    = uint64(8000)
	simMsgSize = 300 // bytes per disclosure (Bloom filter + key + framing)
	simSlots   = 800
)

// TestToggleEngagesUnderConstrainedRadio: a tight radio budget must drive the
// slot duration up off the Tmin floor and hold it there, while keeping the
// backlog bounded (no runaway / overflow).
func TestToggleEngagesUnderConstrainedRadio(t *testing.T) {
	// 100 B/s radio, 300 B disclosures => radio sends only 1 disclosure per
	// 3 s, but 1 is produced every slot. The controller must lengthen the slot.
	tHist, backlogHist := simulateAdaptiveLoop(100, simMsgSize, simSlots, simTMin, simTMax)

	settled := settledMean(tHist)
	peakT := maxU64(tHist)
	peakBacklog := maxInt(backlogHist)
	wantT := 1000.0 * float64(simMsgSize) / 100.0 // 3000 ms fixed point

	if peakT <= simTMin {
		t.Fatalf("toggle never engaged: peak T_cur=%dms stayed at the %dms floor", peakT, simTMin)
	}
	if settled <= float64(simTMin)*1.5 {
		t.Errorf("slot did not settle above the floor: settled=%.0fms (want ~%.0fms)", settled, wantT)
	}
	if rel := math.Abs(settled-wantT) / wantT; rel > 0.30 {
		t.Errorf("settled T=%.0fms not within 30%% of the physical fixed point %.0fms (rel=%.2f)", settled, wantT, rel)
	}
	// Stability: the controller must bound the backlog, not let it run away.
	if peakBacklog > 3*adaptiveQueueCap {
		t.Errorf("backlog runaway: peak=%d (cap=%d) -- controller failed to stabilize", peakBacklog, adaptiveQueueCap)
	}
	t.Logf("constrained radio: settled T=%.0fms (fixed point %.0fms), peak backlog=%d/%d, peak T=%dms",
		settled, wantT, peakBacklog, adaptiveQueueCap, peakT)
}

// TestToggleRelaxesUnderAmpleRadio: a fast radio drains the queue faster than
// it fills, so U_cur stays below target and the controller keeps the slot at
// the Tmin floor -- low latency when there is no congestion.
func TestToggleRelaxesUnderAmpleRadio(t *testing.T) {
	// 6000 B/s => 20 disclosures/slot capacity at Tmin, far above the 1/slot
	// production rate. No pressure, so no reason to lengthen the slot.
	tHist, _ := simulateAdaptiveLoop(6000, simMsgSize, simSlots, simTMin, simTMax)
	settled := settledMean(tHist)
	if settled != float64(simTMin) {
		t.Errorf("slot should stay at the %dms floor under an ample radio, got settled=%.0fms", simTMin, settled)
	}
	t.Logf("ample radio: settled T=%.0fms (floor)", settled)
}

// TestToggleTracksBandwidth: the whole point. Sweep the emulated radio budget
// and confirm the settled slot duration is monotonically longer as bandwidth
// tightens -- the measurable T-vs-bandwidth curve the flat charts were missing.
func TestToggleTracksBandwidth(t *testing.T) {
	bandwidths := []int{100, 150, 200, 300, 600, 1200}
	settled := make([]float64, len(bandwidths))

	t.Logf("radio(B/s) | settled T_cur(ms) | fixed point 1000*%d/bps", simMsgSize)
	for i, bps := range bandwidths {
		tHist, _ := simulateAdaptiveLoop(bps, simMsgSize, simSlots, simTMin, simTMax)
		settled[i] = settledMean(tHist)
		t.Logf("  %6d   |      %6.0f       |   %6.0f", bps, settled[i], 1000.0*float64(simMsgSize)/float64(bps))
	}

	// Monotonic non-increasing: more bandwidth never yields a longer slot.
	for i := 1; i < len(settled); i++ {
		if settled[i] > settled[i-1]+1 { // +1ms guard against rounding ties
			t.Errorf("not monotonic: T(%dB/s)=%.0fms > T(%dB/s)=%.0fms",
				bandwidths[i], settled[i], bandwidths[i-1], settled[i-1])
		}
	}
	// And it must actually span a range, not sit flat like the broken charts.
	if settled[0]-settled[len(settled)-1] < float64(simTMin) {
		t.Errorf("toggle barely moved across the sweep: %.0fms .. %.0fms -- expected a wide spread",
			settled[len(settled)-1], settled[0])
	}
}
