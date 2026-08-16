#!/usr/bin/env python3
"""Corrected T_cur-vs-bandwidth chart for the queue-backpressure toggle.

Replicates the EXACT production control law (internal/rcd:selectNextDuration)
and the same closed loop the headless test uses (rcd_toggle_test.go), sweeps
the emulated radio budget, and plots the settled slot duration. This is the
figure the old flat "Authenticated Throughput vs Emulated Bandwidth" charts
should have produced once the controller can actually feel the queue.

    venv/bin/python benchmarking/generate_toggle_chart.py
"""
import math
import os

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np

# --- constants mirrored from internal/rcd/rcd.go (keep in sync) ---
TARGET_UTILIZATION = 0.5
ADJUSTMENT_FACTOR = 0.25
ADAPTIVE_QUEUE_CAP = 8
MSG_SIZE = 300           # bytes per disclosure (BF + key + framing)
T_MIN, T_MAX = 1000, 8000
SLOTS = 800


def round_half_away(x: float) -> int:
    """Match Go's math.Round (half away from zero), not Python banker's round."""
    return int(math.floor(x + 0.5)) if x >= 0 else int(math.ceil(x - 0.5))


def select_next_duration(qlen: int, qcap: int, t_cur: int) -> int:
    """Exact port of selectNextDuration()."""
    if qcap <= 0:
        return t_cur
    u_cur = qlen / qcap
    delta = u_cur - TARGET_UTILIZATION
    t_next = t_cur * (1 + ADJUSTMENT_FACTOR * delta)
    nxt = round_half_away(t_next)
    return max(T_MIN, min(nxt, T_MAX))


def simulate(radio_bps: float) -> float:
    """Closed loop; returns settled T_cur (mean over the last quarter)."""
    backlog = 0.0
    t = T_MIN
    hist = []
    for _ in range(SLOTS):
        backlog += 1.0                                    # one flush produced
        capacity = radio_bps * (t / 1000.0) / MSG_SIZE    # radio drains this
        backlog -= min(backlog, capacity)
        qlen = max(0, round_half_away(backlog))
        t = select_next_duration(qlen, ADAPTIVE_QUEUE_CAP, t)
        hist.append(t)
    return float(np.mean(hist[3 * SLOTS // 4:]))


def fixed_point(radio_bps: float) -> float:
    """Physical drain==production point T* = 1000*msgSize/bps, clamped."""
    return min(T_MAX, max(T_MIN, 1000.0 * MSG_SIZE / radio_bps))


def main() -> None:
    fine = np.unique(np.round(np.logspace(np.log10(60), np.log10(4000), 80))).astype(int)
    settled = np.array([simulate(b) for b in fine])
    fp = np.array([fixed_point(b) for b in fine])

    marks = [100, 150, 200, 300, 600, 1200]
    mark_settled = [simulate(b) for b in marks]

    out_dir = "plots/toggle_backpressure"
    os.makedirs(out_dir, exist_ok=True)
    path = os.path.join(out_dir, "t_vs_bandwidth.png")

    plt.figure(figsize=(10, 6))
    # old inert controller: pinned at the floor regardless of bandwidth
    plt.axhline(T_MIN, color="#c0392b", ls=":", lw=2,
                label="Old controller (inert) — pinned at $T_{min}$")
    # physical fixed point
    plt.plot(fine, fp, color="#7f8c8d", ls="--", lw=1.8,
             label=r"Physical fixed point $T^*=1000\cdot S/B$")
    # the fixed toggle
    plt.plot(fine, settled, color="#2e6fbf", lw=2.6,
             label="Adaptive $T_{cur}$ (queue-backpressure toggle)")
    plt.scatter(marks, mark_settled, color="#2e6fbf", zorder=5, s=45)
    for b, tv in zip(marks, mark_settled):
        plt.annotate(f"{tv:.0f} ms", (b, tv), textcoords="offset points",
                     xytext=(6, 8), fontsize=9, color="#2e6fbf")

    plt.axhline(T_MAX, color="#95a5a6", lw=1, alpha=0.6)
    plt.text(fine[-1], T_MAX, "  $T_{max}$", va="center", fontsize=9, color="#7f8c8d")
    plt.text(fine[-1], T_MIN, "  $T_{min}$", va="center", fontsize=9, color="#7f8c8d")

    plt.xscale("log")
    plt.xlabel("Emulated radio budget  (bytes/sec, the -radio-bps knob)", fontsize=12)
    plt.ylabel("Settled slot duration  $T_{cur}$  (ms)", fontsize=12)
    plt.title("Adaptive Slot Duration vs. Emulated Bandwidth (Queue-Backpressure Toggle)",
              fontsize=14, fontweight="bold", pad=14)
    plt.ylim(700, T_MAX + 400)
    plt.grid(True, which="both", ls="--", alpha=0.35)
    plt.legend(fontsize=10, loc="upper right")
    plt.tight_layout()
    plt.savefig(path, dpi=200, bbox_inches="tight")
    print(f"[*] wrote {path}")
    print("    marks:", ", ".join(f"{b}B/s->{tv:.0f}ms" for b, tv in zip(marks, mark_settled)))


if __name__ == "__main__":
    main()
