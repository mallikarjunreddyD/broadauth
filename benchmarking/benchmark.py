import argparse
import json
import subprocess
import time

# import uuid
import re
import os

# import signal
import sys
from statistics import mean
from typing import List, Dict, Set, Optional

# --- CONFIGURATION ---
BENCH_DIR = "benchmarks"
FINAL_REPORT = "avg_benchmarks.md"
DURATION = 175  # Seconds per run
RUNS_PER_MODE = 10

# Binaries
RCD_BIN = "./bin/rcd"
OWNER_BIN = "./bin/owner"

# Shared Arguments
ETH_URL = "http://0.0.0.0:8545"
CONTRACT_ADDR = "0x5FbDB2315678afecb367f032d93F642f64180aa3"
OWNER_ADDR = "0.0.0.0:10102"
HASHCHAIN_LEN = "64"
DISCLOSURE_DELAY = "2"

# Owner Specifics
OWNER_PORT = "10102"
OWNER_PRIV_KEY = "59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d"
CM_ADDR = "0.0.0.0:10101"

# Adaptive controller bounds used across all adaptive experiments below.
ADAPTIVE_TMIN_MS = "1000"
ADAPTIVE_TMAX_MS = "8000"
ADAPTIVE_FLAGS = ["-adaptive", "-tmin", ADAPTIVE_TMIN_MS, "-tmax", ADAPTIVE_TMAX_MS]

# The four configurations the reviewer asked to see compared against each
# other: default (non-adaptive) vs. adaptive, crossed with deterministic vs.
# probabilistic mode. Both Chart 1 and Chart 2 sweep all four so the paper can
# show det-baseline-vs-det-adaptive *and* det-adaptive-vs-prob-adaptive from
# the same data. The controller itself is mode-agnostic in the Go code (it
# fires on every slot boundary regardless of Mode) - this is just wiring the
# benchmark harness to actually exercise that.
EXPERIMENT_CONFIGS = [
    ("det-baseline", "deterministic", []),
    ("det-adaptive", "deterministic", ADAPTIVE_FLAGS),
    ("prob-baseline", "probabilistic", []),
    ("prob-adaptive", "probabilistic", ADAPTIVE_FLAGS),
]

LOG_PATTERN = re.compile(
    r"^\s*([\d\.]+)s\s*\|\s*([\d\.]+)\s*B/s\s*\|\s*([\d\.]+)\s*B/s\s*\|\s*([\d\.]+)\s*\|\s*([\d\.]+)\s*\|\s*(\d+)\s*B\s*\|\s*([\d\.]+)\s*\|\s*([\d\.]+)\s*\|\s*([\d\.]+)\s*\|\s*(\d+)\s*\|\s*(\d+)"
)

# [KEYCHAIN] exhausted after 12.345s, refilling
KEYCHAIN_PATTERN = re.compile(r"\[KEYCHAIN\] exhausted after ([\d.]+)s")

# [PROB-ADAPTIVE] t=1.234s T_cur=4000ms U_cur=0.50 Qlen=512 Qcap=1024 T_next=4500ms
ADAPTIVE_TICK_PATTERN = re.compile(
    r"\[PROB-ADAPTIVE\] t=([\d.]+)s T_cur=(\d+)ms U_cur=([\d.]+) Qlen=(\d+) Qcap=(\d+) T_next=(\d+)ms"
)


def setup_directories() -> None:
    if not os.path.exists(BENCH_DIR):
        os.makedirs(BENCH_DIR)
        print(f"[*] Created directory: {BENCH_DIR}")


def start_owner(adaptive: bool = False, num_rcds: int = 20) -> subprocess.Popen[str]:
    print(f"[*] Starting Owner Node on port {OWNER_PORT} (num_rcds={num_rcds})...")
    cmd = [
        OWNER_BIN,
        "-eth-url",
        ETH_URL,
        "-contract",
        CONTRACT_ADDR,
        "-private-key",
        OWNER_PRIV_KEY,
        "-cm-addr",
        CM_ADDR,
        "-disclosure-delay",
        DISCLOSURE_DELAY,
        "-hashchain-len",
        HASHCHAIN_LEN,
        "-port",
        OWNER_PORT,
        "-num-rcds",
        str(num_rcds),
    ]
    if adaptive:
        cmd += ["-adaptive", "-tmin", ADAPTIVE_TMIN_MS, "-tmax", ADAPTIVE_TMAX_MS]

    # We use PIPE to capture stdout so we can read UUIDs
    proc = subprocess.Popen(
        cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, bufsize=1
    )

    time.sleep(2)
    if proc.poll() is not None:
        print("[!!!] Owner failed to start.")
        if proc.stdout:
            print(proc.stdout.read())
        sys.exit(1)
    return proc


def get_uuids_from_owner(owner_proc: subprocess.Popen[str], count: int) -> List[str]:
    print(f"[*] Waiting for {count} UUIDs from Owner...")
    uuids: List[str] = []

    # We also want to save owner output to a log file while reading it
    owner_log_file = open(f"{BENCH_DIR}/owner.log", "w")

    if owner_proc.stdout is None:
        raise RuntimeError("Owner process stdout is None")

    start_time = time.time()
    while time.time() - start_time < 10:  # Wait up to 10s for UUIDs
        line = owner_proc.stdout.readline()
        if not line:
            break

        # Write to log
        owner_log_file.write(line)
        owner_log_file.flush()

        line = line.strip()
        # UUID Regex (simple check)
        if re.match(
            r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", line
        ):
            uuids.append(line)
            if len(uuids) >= count:
                break

    # Start a background thread or process to keep draining owner stdout to file
    # For simplicity in this script, we'll just let the OS buffer fill or hope it's fine.
    # Ideally, we should spawn a reader thread.
    import threading

    def drain_owner_log() -> None:
        if owner_proc.stdout:
            for l in owner_proc.stdout:
                owner_log_file.write(l)
                owner_log_file.flush()
        owner_log_file.close()

    t = threading.Thread(target=drain_owner_log, daemon=True)
    t.start()

    if len(uuids) < count:
        print(f"[!] Warning: Only captured {len(uuids)} UUIDs. Expected {count}.")
        print("    If the Owner didn't print them, RCD(s will fail to co,nnect.")
    else:
        print(f"[*] Successfully captured {len(uuids)} UUIDs.")

    return uuids


def run_rcd_benchmark(
    label: str,
    run_id: int,
    uid: str,
    mode: str = "probabilistic",
    extra_args: Optional[List[str]] = None,
    duration: Optional[int] = None,
    hashchain_len: Optional[str] = None,
) -> str:
    extra_args = extra_args or []
    run_duration = duration if duration is not None else DURATION
    hclen = hashchain_len if hashchain_len is not None else HASHCHAIN_LEN

    log_filename = f"{BENCH_DIR}/run_{run_id:02d}_{label}_{uid}.log"

    print(f"    -> Running {label} (UUID: {uid})")
    print(f"    -> Log: {log_filename}")

    cmd: List[str] = [
        RCD_BIN,
        "-contract",
        CONTRACT_ADDR,
        "-disclosure-delay",
        DISCLOSURE_DELAY,
        "-eth-url",
        ETH_URL,
        "-hashchain-len",
        str(hclen),
        "-owner-addr",
        OWNER_ADDR,
        "-uuid",
        uid,
        "-bench",
        "-mode",
        mode,
    ] + extra_args

    with open(log_filename, "w") as log_file:
        proc = subprocess.Popen(cmd, stdout=log_file, stderr=subprocess.STDOUT)

        try:
            time.sleep(run_duration)
        except KeyboardInterrupt:
            print("\n[!] User interrupted.")
            proc.terminate()
            sys.exit(1)
        finally:
            proc.terminate()
            try:
                proc.wait(timeout=2)
            except subprocess.TimeoutExpired:
                proc.kill()

    return log_filename


def parse_log_file(filepath: str) -> Dict[float, Dict[str, float]]:
    data_points: Dict[float, Dict[str, float]] = {}
    with open(filepath, "r") as f:
        for line in f:
            match = LOG_PATTERN.search(line)
            if match:
                t = float(match.group(1))
                metrics: Dict[str, float] = {
                    "tx_rate": float(match.group(2)),
                    "rx_rate": float(match.group(3)),
                    "msg_rate": float(match.group(4)),
                    "mem": float(match.group(5)),
                    "overhead": int(match.group(6)),
                    "hmac_lat": float(match.group(7)),
                    "verify_lat": float(match.group(8)),
                    "bcast_lat": float(match.group(9)),
                    "verified": int(match.group(10)),
                    "dropped_premature": int(match.group(11)),
                }
                data_points[t] = metrics
    return data_points


def parse_first_keychain_exhaustion(filepath: str) -> Optional[float]:
    """Seconds from RCD start to the first genuine keychain exhaustion
    (the [KEYCHAIN] line only fires on real exhaustion, not the initial fill
    - see internal/rcd/rcd.go CurrentSlotKey)."""
    with open(filepath, "r") as f:
        for line in f:
            m = KEYCHAIN_PATTERN.search(line)
            if m:
                return float(m.group(1))
    return None


def parse_adaptive_ticks(filepath: str) -> List[Dict[str, float]]:
    """Time series of the controller's per-slot-boundary decisions, for the
    step-response chart."""
    ticks: List[Dict[str, float]] = []
    with open(filepath, "r") as f:
        for line in f:
            m = ADAPTIVE_TICK_PATTERN.search(line)
            if m:
                ticks.append(
                    {
                        "t": float(m.group(1)),
                        "t_cur_ms": float(m.group(2)),
                        "u_cur": float(m.group(3)),
                        "qlen": int(m.group(4)),
                        "qcap": int(m.group(5)),
                        "t_next_ms": float(m.group(6)),
                    }
                )
    return ticks


def aggregate_data(
    all_runs_data: List[Dict[float, Dict[str, float]]],
) -> Dict[float, Dict[str, float]]:
    aggregated: Dict[float, Dict[str, float]] = {}
    all_times: Set[float] = set()
    for run in all_runs_data:
        all_times.update(run.keys())

    for t in sorted(list(all_times)):
        valid_runs = [run[t] for run in all_runs_data if t in run]
        if not valid_runs:
            continue

        avg = {k: mean([d[k] for d in valid_runs]) for k in valid_runs[0].keys()}
        aggregated[t] = avg
    return aggregated


def generate_markdown(
    det_data: Dict[float, Dict[str, float]], prob_data: Dict[float, Dict[str, float]]
) -> None:
    def format_table(title: str, data: Dict[float, Dict[str, float]]) -> str:
        lines: List[str] = []
        lines.append(f"### {title}")
        lines.append(
            "| Time (s) | Tx Rate (B/s) | Rx Rate (B/s) | Msg/s | Mem (MB) | Overhead (B) | HMAC (µs) | Verify (µs) | Bcast (µs) |"
        )
        lines.append("|---:|---:|---:|---:|---:|---:|---:|---:|---:|")

        for t in sorted(data.keys()):
            d = data[t]
            lines.append(
                f"| {t:.1f} | {d['tx_rate']:.2f} | {d['rx_rate']:.2f} | {d['msg_rate']:.1f} | "
                f"{d['mem']:.2f} | {int(d['overhead'])} | {d['hmac_lat']:.2f} | "
                f"{d['verify_lat']:.2f} | {d['bcast_lat']:.2f} |"
            )
        return "\n".join(lines)

    content = f"""# RCD Protocol Benchmark Report

**Configuration:**
- Duration per run: {DURATION}s
- Runs per mode: {RUNS_PER_MODE}
- Hashchain Length: {HASHCHAIN_LEN}
- Disclosure Delay: {DISCLOSURE_DELAY}

---

{format_table("Deterministic Mode (Average)", det_data)}

---

{format_table("Probabilistic Mode (Average)", prob_data)}
"""

    with open(FINAL_REPORT, "w") as f:
        f.write(content)
    print(f"\n[*] Report generated: {FINAL_REPORT}")


# ---------------------------------------------------------------------------
# tc-based congestion knobs. Bandwidth-capping (tbf), NOT loss (netem loss) -
# see GUIDE.md/plan discussion: pure packet loss does not block a UDP sender's
# socket write, so it never actually stresses the disclosure queue. Requires
# CAP_NET_ADMIN (root or `sudo`); failures are reported but non-fatal so a
# permission-less environment can still run the rate-only experiments.
# ---------------------------------------------------------------------------


# The RCD's own broadcast/receive port (see internal/broadcast/udp_broadcast.go
# DefaultUDPConfig). This is the only traffic the bandwidth experiment should
# throttle.
RCD_BROADCAST_PORT = 8888


def apply_bandwidth_cap(kbps: int, iface: str = "lo") -> bool:
    """Throttles only RCD broadcast traffic (UDP port 8888), not the whole
    interface. A blanket `tc tbf` on the interface root also throttles the
    owner's JSON-RPC calls to anvil (port 8545) and the owner<->RCD
    hashchain-provisioning TCP traffic (port 10102) - both unrelated to what
    this experiment measures. Under even a modest cap, that starves the
    *initial keychain fill* (which already costs multiple real on-chain
    confirmations even uncapped) well past the run's duration, so every
    mode reports zero throughput regardless of adaptive/baseline or
    deterministic/probabilistic - the bottleneck ends up upstream of
    anything mode-specific.
    """
    clear_bandwidth_cap(iface)
    if kbps <= 0:
        return True

    steps = [
        ["tc", "qdisc", "add", "dev", iface, "root", "handle", "1:", "prio"],
        [
            "tc", "qdisc", "add", "dev", iface, "parent", "1:3", "handle", "30:",
            "tbf", "rate", f"{kbps}kbit", "burst", "32kbit", "latency", "400ms",
        ],
        [
            "tc", "filter", "add", "dev", iface, "protocol", "ip", "parent", "1:0",
            "prio", "1", "u32", "match", "ip", "dport", str(RCD_BROADCAST_PORT), "0xffff",
            "flowid", "1:3",
        ],
        [
            "tc", "filter", "add", "dev", iface, "protocol", "ip", "parent", "1:0",
            "prio", "1", "u32", "match", "ip", "sport", str(RCD_BROADCAST_PORT), "0xffff",
            "flowid", "1:3",
        ],
    ]
    for cmd in steps:
        result = subprocess.run(cmd, stderr=subprocess.PIPE, text=True)
        if result.returncode != 0:
            print(f"[!] Failed to apply {kbps}kbit/s cap on {iface}: {result.stderr.strip()}")
            print("    (tc requires root/CAP_NET_ADMIN - try running with sudo)")
            clear_bandwidth_cap(iface)
            return False
    return True


def clear_bandwidth_cap(iface: str = "lo") -> None:
    subprocess.run(
        ["tc", "qdisc", "del", "dev", iface, "root"],
        stderr=subprocess.DEVNULL,
        stdout=subprocess.DEVNULL,
    )


# ---------------------------------------------------------------------------
# Experiment 1: keychain lifespan vs. injected message rate (Chart 1)
# Fixed bandwidth (uncapped), sweep -msg-rate, compare adaptive vs. baseline.
# ---------------------------------------------------------------------------

LIFESPAN_RATES = [10, 25, 50, 100, 200]
LIFESPAN_HASHCHAIN_LEN = "16"  # small so exhaustion happens within the run
LIFESPAN_DURATION = 60


def run_lifespan_experiment(uuid_iter) -> Dict[str, Dict[int, float]]:
    results: Dict[str, Dict[int, float]] = {label: {} for label, _, _ in EXPERIMENT_CONFIGS}
    print("\n--- Experiment: Keychain lifespan vs. injection rate (4-way: det/prob x baseline/adaptive) ---")
    for rate in LIFESPAN_RATES:
        for label, mode, extra in EXPERIMENT_CONFIGS:
            uid = next(uuid_iter)
            log_file = run_rcd_benchmark(
                f"lifespan_{label}_rate{rate}",
                1,
                uid,
                mode=mode,
                extra_args=["-msg-rate", str(rate)] + extra,
                duration=LIFESPAN_DURATION,
                hashchain_len=LIFESPAN_HASHCHAIN_LEN,
            )
            t = parse_first_keychain_exhaustion(log_file)
            # No exhaustion observed within the run window -> chain outlasted
            # the run; record the run duration as a (lower-bound) lifespan.
            results[label][rate] = t if t is not None else float(LIFESPAN_DURATION)
            print(f"    rate={rate}/s {label}: exhausted after {results[label][rate]:.1f}s")
    return results


# ---------------------------------------------------------------------------
# Experiment 2: throughput vs. emulated bandwidth (Chart 2)
# Fixed saturating -msg-rate, adaptive mode, sweep bandwidth cap via tc tbf.
# ---------------------------------------------------------------------------

BANDWIDTH_CAPS_KBPS = [50, 100, 250, 500, 1000, 0]  # 0 = uncapped
BANDWIDTH_MSG_RATE = 200
BANDWIDTH_DURATION = 45


def run_bandwidth_experiment(
    uuid_iter,
) -> tuple[Dict[str, Dict[int, float]], Dict[str, Dict[int, float]]]:
    """Sweeps a real tc bandwidth cap - the only knob that reliably sustains
    disclosure-queue congestion (raw -msg-rate alone doesn't: local loopback
    processing drains a backlog before the next controller sample sees it).
    Reports two reviewer-requested sender-side metrics from the same sweep,
    since both share the identical "keychain length fixed, congestion
    varied" setup: (1) average throughput, and (2) keychain lifespan -
    how many real seconds a fixed-length keychain lasts as congestion rises.
    """
    throughput: Dict[str, Dict[int, float]] = {label: {} for label, _, _ in EXPERIMENT_CONFIGS}
    lifespan: Dict[str, Dict[int, float]] = {label: {} for label, _, _ in EXPERIMENT_CONFIGS}
    print("\n--- Experiment: Throughput & keychain lifespan vs. emulated bandwidth (4-way: det/prob x baseline/adaptive) ---")
    for kbps in BANDWIDTH_CAPS_KBPS:
        applied = apply_bandwidth_cap(kbps)
        try:
            for label, mode, extra in EXPERIMENT_CONFIGS:
                uid = next(uuid_iter)
                log_file = run_rcd_benchmark(
                    f"bandwidth_{label}_{kbps}kbps",
                    1,
                    uid,
                    mode=mode,
                    extra_args=["-msg-rate", str(BANDWIDTH_MSG_RATE)] + extra,
                    duration=BANDWIDTH_DURATION,
                )
                data = parse_log_file(log_file)
                avg_tx = mean(d["tx_rate"] for d in data.values()) if data else 0.0
                throughput[label][kbps] = avg_tx

                t = parse_first_keychain_exhaustion(log_file)
                # No exhaustion observed within the run window -> the chain
                # outlasted the run; record the run duration as a
                # (lower-bound) lifespan, same convention as
                # run_lifespan_experiment.
                lifespan[label][kbps] = t if t is not None else float(BANDWIDTH_DURATION)

                bw_label = f"{kbps}kbit/s" if kbps > 0 else "uncapped"
                print(
                    f"    bandwidth={bw_label} {label}: avg tx={avg_tx:.1f} B/s, "
                    f"keychain lasted {lifespan[label][kbps]:.1f}s"
                    + ("" if applied else " (cap not applied!)")
                )
        finally:
            clear_bandwidth_cap()
    return throughput, lifespan


# ---------------------------------------------------------------------------
# Experiment 3: step-response (quiet -> bandwidth-capped stress -> quiet)
# Not one of the mentor's two original charts - added to actually show the
# controller stretching under stress and recovering, not just its bounds.
# ---------------------------------------------------------------------------

STEP_QUIET_S = 30
STEP_STRESS_S = 30
STEP_RECOVER_S = 30
STEP_STRESS_BANDWIDTH_KBPS = 50
STEP_MSG_RATE = 150


def run_step_response_experiment(uid: str) -> List[Dict[str, float]]:
    print("\n--- Experiment: Step response (quiet -> stress -> quiet) ---")
    clear_bandwidth_cap()
    log_filename = f"{BENCH_DIR}/step_response_{uid}.log"
    cmd = [
        RCD_BIN,
        "-contract",
        CONTRACT_ADDR,
        "-disclosure-delay",
        DISCLOSURE_DELAY,
        "-eth-url",
        ETH_URL,
        "-hashchain-len",
        HASHCHAIN_LEN,
        "-owner-addr",
        OWNER_ADDR,
        "-uuid",
        uid,
        "-bench",
        "-mode",
        "probabilistic",
        "-msg-rate",
        str(STEP_MSG_RATE),
    ] + ADAPTIVE_FLAGS

    with open(log_filename, "w") as log_file:
        proc = subprocess.Popen(cmd, stdout=log_file, stderr=subprocess.STDOUT)
        try:
            print(f"    quiet for {STEP_QUIET_S}s...")
            time.sleep(STEP_QUIET_S)
            print(f"    applying {STEP_STRESS_BANDWIDTH_KBPS}kbit/s cap for {STEP_STRESS_S}s...")
            apply_bandwidth_cap(STEP_STRESS_BANDWIDTH_KBPS)
            time.sleep(STEP_STRESS_S)
            print(f"    releasing cap, recovering for {STEP_RECOVER_S}s...")
            clear_bandwidth_cap()
            time.sleep(STEP_RECOVER_S)
        finally:
            proc.terminate()
            try:
                proc.wait(timeout=2)
            except subprocess.TimeoutExpired:
                proc.kill()
            clear_bandwidth_cap()

    return parse_adaptive_ticks(log_filename)


def main() -> None:
    parser = argparse.ArgumentParser(description="Prob-Adaptive Inf-TESLA++ benchmark suite")
    parser.add_argument(
        "--experiment",
        choices=["baseline", "lifespan", "bandwidth", "step", "all"],
        default="all",
        help="Which experiment(s) to run (default: all)",
    )
    args = parser.parse_args()

    setup_directories()

    run_baseline = args.experiment in ("baseline", "all")
    run_lifespan = args.experiment in ("lifespan", "all")
    run_bandwidth = args.experiment in ("bandwidth", "all")
    run_step = args.experiment in ("step", "all")

    total_uuids = 0
    if run_baseline:
        total_uuids += 2 * RUNS_PER_MODE
    if run_lifespan:
        total_uuids += len(EXPERIMENT_CONFIGS) * len(LIFESPAN_RATES)
    if run_bandwidth:
        total_uuids += len(EXPERIMENT_CONFIGS) * len(BANDWIDTH_CAPS_KBPS)
    if run_step:
        total_uuids += 1

    owner_proc = start_owner(
        adaptive=(run_lifespan or run_bandwidth or run_step),
        num_rcds=max(total_uuids, 1),
    )

    try:
        uuids = get_uuids_from_owner(owner_proc, total_uuids)
        if len(uuids) < total_uuids:
            print("[!] Not enough UUIDs captured to run the full requested suite.")
        uuid_iter = iter(uuids)

        if run_baseline:
            det_uuids = [next(uuid_iter) for _ in range(RUNS_PER_MODE)]
            prob_uuids = [next(uuid_iter) for _ in range(RUNS_PER_MODE)]

            det_raw_data: List[Dict[float, Dict[str, float]]] = []
            prob_raw_data: List[Dict[float, Dict[str, float]]] = []

            print(
                f"\n[*] Starting Baseline Benchmark Suite ({RUNS_PER_MODE} runs per mode, {DURATION}s each)"
            )

            print("\n--- Phase 1: Deterministic Mode ---")
            for i, uid in enumerate(det_uuids):
                run_num = i + 1
                print(f"[*] Run {run_num}/{RUNS_PER_MODE}...")
                log_file = run_rcd_benchmark("deterministic", run_num, uid, mode="deterministic")
                det_raw_data.append(parse_log_file(log_file))
                time.sleep(1)

            print("\n--- Phase 2: Probabilistic Mode ---")
            for i, uid in enumerate(prob_uuids):
                run_num = i + 1
                print(f"[*] Run {run_num}/{RUNS_PER_MODE}...")
                log_file = run_rcd_benchmark("probabilistic", run_num, uid, mode="probabilistic")
                prob_raw_data.append(parse_log_file(log_file))
                time.sleep(1)

            print("\n[*] Aggregating baseline data...")
            det_avg = aggregate_data(det_raw_data)
            prob_avg = aggregate_data(prob_raw_data)
            generate_markdown(det_avg, prob_avg)

        if run_lifespan:
            lifespan_results = run_lifespan_experiment(uuid_iter)
            with open(f"{BENCH_DIR}/lifespan_results.json", "w") as f:
                json.dump(lifespan_results, f, indent=2)
            print(f"[*] Saved {BENCH_DIR}/lifespan_results.json")

        if run_bandwidth:
            bandwidth_results, bandwidth_lifespan_results = run_bandwidth_experiment(uuid_iter)
            with open(f"{BENCH_DIR}/bandwidth_results.json", "w") as f:
                json.dump(bandwidth_results, f, indent=2)
            print(f"[*] Saved {BENCH_DIR}/bandwidth_results.json")
            with open(f"{BENCH_DIR}/bandwidth_lifespan_results.json", "w") as f:
                json.dump(bandwidth_lifespan_results, f, indent=2)
            print(f"[*] Saved {BENCH_DIR}/bandwidth_lifespan_results.json")

        if run_step:
            step_uid = next(uuid_iter)
            ticks = run_step_response_experiment(step_uid)
            with open(f"{BENCH_DIR}/step_response_ticks.json", "w") as f:
                json.dump(ticks, f, indent=2)
            print(f"[*] Saved {BENCH_DIR}/step_response_ticks.json ({len(ticks)} ticks)")

    finally:
        print("[*] Stopping Owner Node...")
        owner_proc.terminate()
        try:
            owner_proc.wait(timeout=2)
        except Exception:
            owner_proc.kill()


if __name__ == "__main__":
    main()
