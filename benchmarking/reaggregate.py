#!/usr/bin/env python3
"""Recompute steady-state stats from the saved per-run logs with a robust
estimator (median of the last 40% of ticks), without re-running the sweep.

The live sweep already wrote results/e2e/rcd_<bps>bps_i<it>.log and a JSON with
metadata. Here we re-parse the raw logs so the "settled" value reflects the
plateau/limit-cycle centre rather than a mean dragged down by the ramp (which
matters at low budgets where an 8 s slot yields only a handful of ticks).

    venv/bin/python benchmarking/reaggregate.py
"""
import glob
import json
import re
import sys

SUFFIX = sys.argv[1] if len(sys.argv) > 1 else ""
TAG = f"_{SUFFIX}" if SUFFIX else ""
OUT = f"results/e2e/radio_sweep{TAG}.json"
RE_TICK = re.compile(r"\[PROB-ADAPTIVE\].*T_cur=(\d+)ms U_cur=[\d.]+ Qlen=(\d+)")
RE_BENCH = re.compile(r"\|\s*(\d+)\s*\|\s*(\d+)\s*$")
TAIL = 0.40  # fraction of the run (from the end) treated as steady state


def median(xs):
    s = sorted(xs); n = len(s)
    return s[n // 2] if n % 2 else (s[n // 2 - 1] + s[n // 2]) / 2


def parse(path):
    t, q, ver, prem = [], [], 0, 0
    for line in open(path):
        m = RE_TICK.search(line)
        if m:
            t.append(int(m.group(1))); q.append(int(m.group(2))); continue
        b = RE_BENCH.search(line)
        if b:
            ver, prem = int(b.group(1)), int(b.group(2))
    if not t:
        return None
    k = max(1, int(len(t) * TAIL))
    return {"settled_t": median(t[-k:]), "peak_t": max(t),
            "settled_qlen": median(q[-k:]), "peak_qlen": max(q),
            "verified": ver, "premature_drop": prem,
            "n_ticks": len(t), "t_hist": t, "q_hist": q}


def main():
    d = json.load(open(OUT))
    for bps in list(d["runs"]):
        logs = sorted(glob.glob(f"results/e2e/rcd{TAG}_{bps}bps_i*.log"))
        per = [parse(p) for p in logs]
        per = [r for r in per if r]
        if not per:
            continue
        st = [r["settled_t"] for r in per]
        med = median(st)
        rep = min(per, key=lambda r: abs(r["settled_t"] - med))
        d["runs"][bps] = {
            "settled_t": med, "settled_t_iters": st,
            "peak_t": max(r["peak_t"] for r in per),
            "settled_qlen": median([r["settled_qlen"] for r in per]),
            "settled_qlen_iters": [r["settled_qlen"] for r in per],
            "peak_qlen": max(r["peak_qlen"] for r in per),
            "verified": sum(r["verified"] for r in per) / len(per),
            "verified_iters": [r["verified"] for r in per],
            "premature_drop": sum(r["premature_drop"] for r in per) / len(per),
            "premature_iters": [r["premature_drop"] for r in per],
            "iters": len(per), "t_hist": rep["t_hist"], "q_hist": rep["q_hist"],
        }
    d["settled_estimator"] = f"median of last {int(TAIL*100)}% of ticks"
    json.dump(d, open(OUT, "w"), indent=2)
    print("[*] re-aggregated", OUT)
    for b in sorted(d["runs"], key=int):
        r = d["runs"][b]
        print(f"  {int(b):4d} B/s  settled_T={r['settled_t']:.0f} peak_T={r['peak_t']} "
              f"settled_Q={r['settled_qlen']:.1f} verified~{r['verified']:.0f} premature~{r['premature_drop']:.0f}")


if __name__ == "__main__":
    main()
