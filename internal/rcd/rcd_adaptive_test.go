package rcd

import "testing"

func TestSelectNextDuration(t *testing.T) {
	const tMin, tMax = uint64(1000), uint64(8000)

	tests := []struct {
		name       string
		qlen, qcap int
		tCur       uint64
		want       uint64
	}{
		{
			name: "empty queue speeds up by max step",
			qlen: 0, qcap: 100, tCur: 4000,
			// U_cur=0, Delta=-0.5, step=-12.5% -> 4000*0.875=3500
			want: 3500,
		},
		{
			name: "half-full queue holds steady",
			qlen: 50, qcap: 100, tCur: 4000,
			// U_cur=0.5, Delta=0, no change
			want: 4000,
		},
		{
			name: "full queue slows down by max step",
			qlen: 100, qcap: 100, tCur: 4000,
			// U_cur=1.0, Delta=+0.5, step=+12.5% -> 4000*1.125=4500
			want: 4500,
		},
		{
			name: "clamps at Tmax even under sustained full-queue pressure",
			qlen: 100, qcap: 100, tCur: 7900,
			// 7900*1.125=8887.5, clamped to Tmax=8000
			want: 8000,
		},
		{
			name: "clamps at Tmin even under sustained empty-queue pressure",
			qlen: 0, qcap: 100, tCur: 1050,
			// 1050*0.875=918.75, clamped to Tmin=1000
			want: 1000,
		},
		{
			name: "zero-capacity queue is a no-op (division-by-zero guard)",
			qlen: 0, qcap: 0, tCur: 4000,
			want: 4000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectNextDuration(tt.qlen, tt.qcap, tt.tCur, tMin, tMax)
			if got != tt.want {
				t.Errorf("selectNextDuration(%d,%d,%d,%d,%d) = %d, want %d",
					tt.qlen, tt.qcap, tt.tCur, tMin, tMax, got, tt.want)
			}
		})
	}
}

func TestSelectNextDurationMaxStepIsTwelvePointFivePercent(t *testing.T) {
	// The EIP-1559-style rule must never move Tcur by more than 12.5% in a
	// single call, regardless of Tcur's current value, since Delta is bounded
	// to [-0.5, 0.5] by construction (Qlen/Qcap in [0,1], target=0.5).
	const tMin, tMax = uint64(1), uint64(1_000_000_000)

	for _, tCur := range []uint64{1000, 5000, 8000, 100000} {
		up := selectNextDuration(100, 100, tCur, tMin, tMax)
		down := selectNextDuration(0, 100, tCur, tMin, tMax)

		wantUp := uint64(float64(tCur) * 1.125)
		wantDown := uint64(float64(tCur) * 0.875)

		if up != wantUp {
			t.Errorf("full-queue step from %d = %d, want %d (+12.5%%)", tCur, up, wantUp)
		}
		if down != wantDown {
			t.Errorf("empty-queue step from %d = %d, want %d (-12.5%%)", tCur, down, wantDown)
		}
	}
}
