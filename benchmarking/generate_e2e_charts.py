#!/usr/bin/env python3
"""Final paper charts from the LIVE end-to-end radio-budget sweep.

Reads results/e2e/radio_sweep.json (produced by radio_sweep.py running the real
anvil+cm+owner+RCD stack) and renders publication figures to plots/e2e/:

  fig1_slot_vs_bandwidth.png   settled/peak T_cur vs radio budget (the toggle)
  fig2_backlog_vs_bandwidth.png disclosure backlog vs radio budget (survivability)
  fig3_verification_outcomes.png verified vs premature-drop (receiver outcomes)
  fig4_step_timeseries.png     T_cur(t) & backlog(t) for one run (engagement)

  venv/bin/python benchmarking/generate_e2e_charts.py
"""
import json
import os
import sys

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

# Optional args: [input_json] [output_dir]. Defaults reproduce the adaptive set.
IN = sys.argv[1] if len(sys.argv) > 1 else "results/e2e/radio_sweep.json"
OUT = sys.argv[2] if len(sys.argv) > 2 else "plots/e2e"
BLUE, ORANGE, GREY, RED, GREEN = "#2e6fbf", "#e08a1e", "#7f8c8d", "#c0392b", "#2e8b57"


def load():
    d = json.load(open(IN))
    runs = d["runs"]
    bws = sorted(int(b) for b in runs)
    return d, runs, bws


def err(runs, bws, key_iters, key_val):
    """Asymmetric [below, above] error from per-iteration spread (min..max)."""
    lo, hi = [], []
    for b in bws:
        r = runs[str(b)]
        vals = r.get(key_iters)
        v = r[key_val]
        if vals and len(vals) > 1:
            lo.append(v - min(vals)); hi.append(max(vals) - v)
        else:
            lo.append(0); hi.append(0)
    return [lo, hi]


def fig1_slot(d, runs, bws):
    tmin, tmax = d["tmin"], d["tmax"]
    settled = [runs[str(b)]["settled_t"] for b in bws]
    peak = [runs[str(b)]["peak_t"] for b in bws]
    plt.figure(figsize=(9, 5.5))
    plt.axhline(tmax, color=GREY, lw=1, alpha=.6); plt.axhline(tmin, color=GREY, lw=1, alpha=.6)
    plt.plot(bws, peak, color=BLUE, ls="--", lw=1.6, marker="^", ms=6, alpha=.55, label="peak $T_{cur}$")
    plt.errorbar(bws, settled, yerr=err(runs, bws, "settled_t_iters", "settled_t"),
                 color=BLUE, lw=2.6, marker="o", ms=7, capsize=4, label="settled $T_{cur}$ (median $\\pm$ range)")
    for b, t in zip(bws, settled):
        plt.annotate(f"{t:.0f}", (b, t), textcoords="offset points", xytext=(0, 9),
                     ha="center", fontsize=8, color=BLUE)
    plt.text(bws[-1], tmax, "  $T_{max}$", va="center", fontsize=9, color=GREY)
    plt.text(bws[-1], tmin, "  $T_{min}$", va="center", fontsize=9, color=GREY)
    plt.xscale("log"); plt.xlabel("Auth-channel radio budget (bytes/sec)", fontsize=12)
    plt.ylabel("Slot duration $T_{cur}$ (ms)", fontsize=12)
    plt.title("Adaptive Slot Duration vs. Radio Budget (measured, end-to-end)",
              fontsize=13, fontweight="bold")
    plt.ylim(tmin - 400, tmax + 500); plt.grid(True, which="both", ls="--", alpha=.35)
    plt.legend(fontsize=10); plt.tight_layout()
    _save("fig1_slot_vs_bandwidth.png")


def fig2_backlog(d, runs, bws):
    settled_q = [runs[str(b)]["settled_qlen"] for b in bws]
    peak_q = [runs[str(b)]["peak_qlen"] for b in bws]
    plt.figure(figsize=(9, 5.5))
    plt.axhline(4, color=GREEN, ls=":", lw=1.8, label="controller target ($U$=0.5, backlog 4)")
    plt.axhline(8, color=ORANGE, ls="--", lw=1.5, label="$Q_{cap}$ = 8")
    plt.plot(bws, peak_q, color=RED, ls="--", lw=1.6, marker="^", ms=6, alpha=.6, label="peak backlog")
    plt.errorbar(bws, settled_q, yerr=err(runs, bws, "settled_qlen_iters", "settled_qlen"),
                 color=BLUE, lw=2.6, marker="o", ms=7, capsize=4, label="settled backlog (median $\\pm$ range)")
    plt.xscale("log"); plt.xlabel("Auth-channel radio budget (bytes/sec)", fontsize=12)
    plt.ylabel("Pending disclosure backlog $Q_{len}$", fontsize=12)
    plt.title("Disclosure Backlog vs. Radio Budget (measured, end-to-end)",
              fontsize=13, fontweight="bold")
    plt.grid(True, which="both", ls="--", alpha=.35); plt.legend(fontsize=10)
    plt.tight_layout(); _save("fig2_backlog_vs_bandwidth.png")


def fig3_verification(d, runs, bws):
    verified = [runs[str(b)]["verified"] for b in bws]
    premature = [runs[str(b)]["premature_drop"] for b in bws]
    x = range(len(bws)); w = 0.4
    ve = err(runs, bws, "verified_iters", "verified")
    pe = err(runs, bws, "premature_iters", "premature_drop")
    plt.figure(figsize=(9, 5.5))
    plt.bar([i - w/2 for i in x], verified, w, color=GREEN, label="Verified",
            yerr=ve, capsize=3, error_kw={"alpha": .5})
    plt.bar([i + w/2 for i in x], premature, w, color=RED, label="Dropped (premature disclosure)",
            yerr=pe, capsize=3, error_kw={"alpha": .5})
    plt.xticks(list(x), [str(b) for b in bws])
    plt.xlabel("Auth-channel radio budget (bytes/sec)", fontsize=12)
    plt.ylabel("Messages (per %ds run)" % d["duration_s"], fontsize=12)
    plt.title("Receiver Verification Outcomes vs. Radio Budget (measured)",
              fontsize=13, fontweight="bold")
    plt.grid(True, axis="y", ls="--", alpha=.35); plt.legend(fontsize=10)
    plt.tight_layout(); _save("fig3_verification_outcomes.png")


def fig4_timeseries(d, runs, bws):
    # pick a run that actually ramps (T climbs above Tmin but not instantly railed)
    tmin = d["tmin"]
    cand = [b for b in bws if runs[str(b)]["peak_t"] > tmin and runs[str(b)]["settled_t"] < d["tmax"]]
    b = cand[len(cand)//2] if cand else bws[len(bws)//2]
    r = runs[str(b)]
    t_hist, q_hist = r["t_hist"], r["q_hist"]
    slots = range(len(t_hist))
    fig, ax1 = plt.subplots(figsize=(9, 5.5))
    ax1.plot(slots, t_hist, color=BLUE, lw=2.4, marker="o", ms=3, label="$T_{cur}$ (ms)")
    ax1.set_xlabel("Slot (controller tick)", fontsize=12)
    ax1.set_ylabel("$T_{cur}$ (ms)", color=BLUE, fontsize=12); ax1.tick_params(axis="y", labelcolor=BLUE)
    ax2 = ax1.twinx()
    ax2.plot(slots, q_hist, color=RED, lw=1.8, alpha=.7, label="backlog $Q_{len}$")
    ax2.axhline(4, color=GREEN, ls=":", lw=1.5)
    ax2.set_ylabel("Disclosure backlog $Q_{len}$", color=RED, fontsize=12); ax2.tick_params(axis="y", labelcolor=RED)
    ax1.set_title(f"Toggle Engagement Over Time @ {b} B/s (measured)", fontsize=13, fontweight="bold")
    ax1.grid(True, ls="--", alpha=.3)
    fig.tight_layout(); _save("fig4_step_timeseries.png")


def _save(name):
    os.makedirs(OUT, exist_ok=True)
    p = os.path.join(OUT, name)
    plt.savefig(p, dpi=200, bbox_inches="tight"); plt.close()
    print(f"[*] wrote {p}")


def main():
    d, runs, bws = load()
    print(f"[*] {len(bws)} bandwidth points: {bws}")
    fig1_slot(d, runs, bws)
    fig2_backlog(d, runs, bws)
    fig3_verification(d, runs, bws)
    fig4_timeseries(d, runs, bws)


if __name__ == "__main__":
    main()
