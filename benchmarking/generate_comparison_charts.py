#!/usr/bin/env python3
"""Overlay charts: adaptive toggle vs. fixed-slot baselines (no toggle).

Reads the three per-arm sweep files produced by radio_sweep.py + reaggregate.py:
  results/e2e/radio_sweep_adaptive.json   (T_min..T_max, the controller)
  results/e2e/radio_sweep_fixedfast.json  (T pinned at T_min = 1000 ms)
  results/e2e/radio_sweep_fixedslow.json  (T pinned at T_max = 8000 ms)
and renders comparison figures to plots/e2e_compare/.

  venv/bin/python benchmarking/generate_comparison_charts.py
"""
import json
import os

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

D = "results/e2e"
OUT = "plots/e2e_compare"
DELAY = 2  # disclosure delay d, for auth latency = d * T

ARMS = [
    ("adaptive",  "Adaptive (toggle)",     "#2e6fbf", "-",   "o"),
    ("fixedfast", "Fixed-fast ($T_{min}$)", "#e08a1e", "--",  "s"),
    ("fixedslow", "Fixed-slow ($T_{max}$)", "#2e8b57", "-.",  "^"),
]


def load(arm):
    p = f"{D}/radio_sweep_{arm}.json"
    if not os.path.exists(p):
        return None
    d = json.load(open(p))
    bws = sorted(int(b) for b in d["runs"])
    return d, bws


def series(d, bws, key):
    return [d["runs"][str(b)][key] for b in bws]


def _save(name):
    os.makedirs(OUT, exist_ok=True)
    p = os.path.join(OUT, name)
    plt.savefig(p, dpi=200, bbox_inches="tight"); plt.close()
    print(f"[*] wrote {p}")


def panel(key, ylabel, title, fname, log_y=False, transform=None):
    plt.figure(figsize=(9, 5.5))
    for arm, label, color, ls, mk in ARMS:
        got = load(arm)
        if not got:
            print(f"    (skip {arm}: no data)"); continue
        d, bws = got
        y = series(d, bws, key)
        if transform:
            y = [transform(v) for v in y]
        plt.plot(bws, y, color=color, ls=ls, marker=mk, ms=6, lw=2.2, label=label)
    plt.xscale("log")
    if log_y:
        plt.yscale("log")
    plt.xlabel("Auth-channel radio budget (bytes/sec)", fontsize=12)
    plt.ylabel(ylabel, fontsize=12)
    plt.title(title, fontsize=13, fontweight="bold")
    plt.grid(True, which="both", ls="--", alpha=.35)
    plt.legend(fontsize=10)
    plt.tight_layout()
    _save(fname)


def main():
    os.makedirs(OUT, exist_ok=True)
    # 1) slot duration: adaptive rides between the two fixed rails
    panel("settled_t", "Slot duration $T_{cur}$ (ms)",
          "Slot Duration vs. Radio Budget: Adaptive vs. Fixed",
          "cmp_slot.png")
    # 2) verified messages: the survivability headline
    panel("verified", "Verified messages (per 90 s run)",
          "Authentication Throughput vs. Radio Budget: Adaptive vs. Fixed",
          "cmp_verified.png")
    # 3) disclosure backlog: who overflows first
    panel("peak_qlen", "Peak disclosure backlog $Q_{len}$",
          "Disclosure Backlog vs. Radio Budget: Adaptive vs. Fixed",
          "cmp_backlog.png")
    # 4) authentication latency d*T: the honest cost / frontier
    panel("settled_t", "Authentication latency $d\\cdot T$ (ms)",
          "Authentication Latency vs. Radio Budget: Adaptive vs. Fixed",
          "cmp_latency.png", transform=lambda t: DELAY * t)


if __name__ == "__main__":
    main()
