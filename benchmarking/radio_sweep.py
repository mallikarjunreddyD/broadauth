#!/usr/bin/env python3
"""End-to-end radio-budget sweep for the queue-backpressure toggle.

Runs the LIVE system (anvil + cm + owner must already be up) one RCD at a time,
sweeping -radio-bps. Each run is a real probabilistic+adaptive RCD broadcasting
over the loopback UDP radio; the throttled auth channel makes the disclosure
backlog build, so the controller lengthens T_cur. Parses the [PROB-ADAPTIVE]
ticks (T_cur, Qlen/U_cur) and the -bench table (Verified, PrematureDrop) and
writes results/e2e/radio_sweep.json.

  # prerequisites: anvil, cm, owner running (see run.txt); owner prints UUIDs
  venv/bin/python benchmarking/radio_sweep.py [DURATION] [bps,bps,...]
"""
import json
import os
import re
import subprocess
import sys
import time

CONTRACT = "0x5FbDB2315678afecb367f032d93F642f64180aa3"
ETH_URL = "http://0.0.0.0:8545"
OWNER_ADDR = "0.0.0.0:10102"
OWNER_LOG = "/tmp/owner.log"
RCD_BIN = "./bin/rcd"
HASHCHAIN_LEN = "512"
DISCLOSURE_DELAY = "2"
TMIN, TMAX = "1000", "8000"
MSG_RATE = "50"
OUT_DIR = "results/e2e"

DEFAULT_DURATION = 90
DEFAULT_BPS = [80, 120, 160, 240, 400, 700, 1200]

RE_TICK = re.compile(
    r"\[PROB-ADAPTIVE\].*T_cur=(\d+)ms U_cur=([\d.]+) Qlen=(\d+) Qcap=(\d+) T_next=(\d+)ms")
# -bench table row: last two numeric columns are Verified and PrematureDrop
RE_BENCH = re.compile(r"\|\s*(\d+)\s*\|\s*(\d+)\s*$")


def uuid_pool(path):
    pat = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
    with open(path) as f:
        return [ln.strip() for ln in f if pat.match(ln.strip())]


def run_one(uid, bps, duration, log_path, tmin=TMIN, tmax=TMAX):
    with open(log_path, "w") as f:
        proc = subprocess.Popen(
            [RCD_BIN, "-contract", CONTRACT, "-eth-url", ETH_URL,
             "-owner-addr", OWNER_ADDR, "-uuid", uid,
             "-hashchain-len", HASHCHAIN_LEN, "-disclosure-delay", DISCLOSURE_DELAY,
             "-mode", "probabilistic", "-adaptive", "-tmin", str(tmin), "-tmax", str(tmax),
             "-msg-rate", MSG_RATE, "-radio-bps", str(bps), "-bench",
             "-simulation-time", f"{duration + 5}s"],
            stdout=f, stderr=subprocess.STDOUT)
        time.sleep(duration + 8)
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill(); proc.wait()


def parse(log_path):
    t_hist, q_hist = [], []
    verified, premature = 0, 0
    with open(log_path) as f:
        for line in f:
            m = RE_TICK.search(line)
            if m:
                t_hist.append(int(m.group(1)))
                q_hist.append(int(m.group(3)))
                continue
            b = RE_BENCH.search(line)
            if b:
                verified, premature = int(b.group(1)), int(b.group(2))
    if not t_hist:
        return None
    # steady state = skip the first 25% (ramp/warm-up), average the rest.
    warm = len(t_hist) // 4
    steady_t = t_hist[warm:] or t_hist
    steady_q = q_hist[warm:] or q_hist
    return {
        "settled_t": sum(steady_t) / len(steady_t),
        "peak_t": max(t_hist),
        "settled_qlen": sum(steady_q) / len(steady_q),
        "peak_qlen": max(q_hist),
        "verified": verified,
        "premature_drop": premature,
        "n_ticks": len(t_hist),
        "t_hist": t_hist,
        "q_hist": q_hist,
    }


def median(xs):
    s = sorted(xs)
    n = len(s)
    return s[n // 2] if n % 2 else (s[n // 2 - 1] + s[n // 2]) / 2


def aggregate(per_iter):
    """Combine ITER runs of one bandwidth: median for T/backlog (robust to the
    controller's hunting), mean for verification, and keep the timeseries of the
    run whose settled_t is closest to the median (the representative run)."""
    st = [r["settled_t"] for r in per_iter]
    med_st = median(st)
    rep = min(per_iter, key=lambda r: abs(r["settled_t"] - med_st))
    return {
        "settled_t": med_st,
        "settled_t_iters": st,
        "peak_t": max(r["peak_t"] for r in per_iter),
        "settled_qlen": median([r["settled_qlen"] for r in per_iter]),
        "settled_qlen_iters": [r["settled_qlen"] for r in per_iter],
        "peak_qlen": max(r["peak_qlen"] for r in per_iter),
        "verified": sum(r["verified"] for r in per_iter) / len(per_iter),
        "verified_iters": [r["verified"] for r in per_iter],
        "premature_drop": sum(r["premature_drop"] for r in per_iter) / len(per_iter),
        "premature_iters": [r["premature_drop"] for r in per_iter],
        "iters": len(per_iter),
        "t_hist": rep["t_hist"],
        "q_hist": rep["q_hist"],
    }


def main():
    duration = int(sys.argv[1]) if len(sys.argv) > 1 else DEFAULT_DURATION
    bps_list = ([int(x) for x in sys.argv[2].split(",")]
                if len(sys.argv) > 2 else DEFAULT_BPS)
    iters = int(sys.argv[3]) if len(sys.argv) > 3 else 1
    tmin = sys.argv[4] if len(sys.argv) > 4 else TMIN
    tmax = sys.argv[5] if len(sys.argv) > 5 else TMAX
    # optional arm label -> namespaces the JSON and per-run logs so multiple
    # arms (adaptive / fixed_fast / fixed_slow) don't overwrite each other.
    suffix = sys.argv[6] if len(sys.argv) > 6 else ""
    tag = f"_{suffix}" if suffix else ""
    os.makedirs(OUT_DIR, exist_ok=True)
    pool = uuid_pool(OWNER_LOG)
    need = len(bps_list) * iters
    if len(pool) < need:
        print(f"[!] only {len(pool)} UUIDs for {need} runs"); sys.exit(1)

    results = {"duration_s": duration, "msg_rate": int(MSG_RATE), "iters": iters,
               "tmin": int(tmin), "tmax": int(tmax), "arm": suffix or "adaptive", "runs": {}}
    print(f"[*] sweep: duration={duration}s iters={iters} tmin={tmin} tmax={tmax} "
          f"arm={suffix or 'adaptive'} bps={bps_list}")
    k = 0
    for bps in bps_list:
        per_iter = []
        for it in range(iters):
            uid = pool[k]; k += 1
            log_path = f"{OUT_DIR}/rcd{tag}_{bps}bps_i{it}.log"
            print(f"  {bps} B/s  iter {it+1}/{iters}  (uuid {uid[:8]}) ...", flush=True)
            run_one(uid, bps, duration, log_path, tmin, tmax)
            r = parse(log_path)
            if r is None:
                print("      no ticks parsed!"); continue
            per_iter.append(r)
            print(f"      settled T={r['settled_t']:.0f} peak_T={r['peak_t']} "
                  f"settled_Q={r['settled_qlen']:.1f} verified={r['verified']} premature={r['premature_drop']}")
        if per_iter:
            agg = aggregate(per_iter)
            results["runs"][str(bps)] = agg
            print(f"  => {bps} B/s AGG: settled_T={agg['settled_t']:.0f} "
                  f"(iters {['%.0f'%x for x in agg['settled_t_iters']]}) "
                  f"verified~{agg['verified']:.0f}")

    path = f"{OUT_DIR}/radio_sweep{tag}.json"
    with open(path, "w") as f:
        json.dump(results, f, indent=2)
    print(f"[*] wrote {path}")


if __name__ == "__main__":
    main()
