import subprocess
import time
import re
import os
import sys
import threading
from statistics import mean
from typing import List, Dict, Set

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
HASHCHAIN_LEN = "512"
DISCLOSURE_DELAY = "2"

# Owner Specifics
OWNER_PORT = "10102"
OWNER_PRIV_KEY = "59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d"
CM_ADDR = "0.0.0.0:10101"

LOG_PATTERN = re.compile(
    r"^\s*([\d\.]+)s\s*\|\s*([\d\.]+)\s*B/s\s*\|\s*([\d\.]+)\s*B/s\s*\|\s*([\d\.]+)\s*\|\s*([\d\.]+)\s*\|\s*(\d+)\s*B\s*\|\s*([\d\.]+)\s*\|\s*([\d\.]+)\s*\|\s*([\d\.]+)"
)


def setup_directories() -> None:
    if not os.path.exists(BENCH_DIR):
        os.makedirs(BENCH_DIR)
        print(f"[*] Created directory: {BENCH_DIR}")


def start_owner() -> subprocess.Popen[str]:
    print(f"[*] Starting Owner Node on port {OWNER_PORT}...")
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
    ]

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
    owner_log_file = open(f"{BENCH_DIR}/owner.log", "w")

    if owner_proc.stdout is None:
        raise RuntimeError("Owner process stdout is None")

    start_time = time.time()
    while time.time() - start_time < 15:  # Increased wait time for 4x UUIDs
        line = owner_proc.stdout.readline()
        if not line:
            break

        owner_log_file.write(line)
        owner_log_file.flush()
        line = line.strip()

        if re.match(
            r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", line
        ):
            uuids.append(line)
            if len(uuids) >= count:
                break

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
    else:
        print(f"[*] Successfully captured {len(uuids)} UUIDs.")
    return uuids


def run_rcd_benchmark(mode: str, run_id: int, uid: str) -> str:
    log_filename = f"{BENCH_DIR}/run_{run_id:02d}_{mode}_{uid}.log"
    print(f"    -> Running {mode} (UUID: {uid})")

    cmd: List[str] = [
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
        mode,
    ]

    # Add adaptive bounds if running adaptive modes
    if mode in ["adaptive", "probadaptive"]:
        cmd.extend(["-t-min", "12000", "-t-max", "24000"])

    with open(log_filename, "w") as log_file:
        proc = subprocess.Popen(cmd, stdout=log_file, stderr=subprocess.STDOUT)
        try:
            time.sleep(DURATION)
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
                data_points[t] = {
                    "tx_rate": float(match.group(2)),
                    "rx_rate": float(match.group(3)),
                    "msg_rate": float(match.group(4)),
                    "mem": float(match.group(5)),
                    "overhead": int(match.group(6)),
                    "hmac_lat": float(match.group(7)),
                    "verify_lat": float(match.group(8)),
                    "bcast_lat": float(match.group(9)),
                }
    return data_points


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
        aggregated[t] = {
            k: mean([d[k] for d in valid_runs]) for k in valid_runs[0].keys()
        }
    return aggregated


def generate_markdown(
    det_data: Dict[float, Dict[str, float]],
    prob_data: Dict[float, Dict[str, float]],
    adapt_data: Dict[float, Dict[str, float]],
    probadapt_data: Dict[float, Dict[str, float]],
) -> None:
    def format_table(title: str, data: Dict[float, Dict[str, float]]) -> str:
        lines: List[str] = [f"### {title}"]
        lines.append(
            "| Time (s) | Tx Rate (B/s) | Rx Rate (B/s) | Msg/s | Mem (MB) | Overhead (B) | HMAC (µs) | Verify (µs) | Bcast (µs) |"
        )
        lines.append("|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
        for t in sorted(data.keys()):
            d = data[t]
            lines.append(
                f"| {t:.1f} | {d['tx_rate']:.2f} | {d['rx_rate']:.2f} | {d['msg_rate']:.1f} | {d['mem']:.2f} | {int(d['overhead'])} | {d['hmac_lat']:.2f} | {d['verify_lat']:.2f} | {d['bcast_lat']:.2f} |"
            )
        return "\n".join(lines)

    content = f"""# RCD Protocol Benchmark Report

**Configuration:**
- Duration per run: {DURATION}s
- Runs per mode: {RUNS_PER_MODE}
- Hashchain Length: {HASHCHAIN_LEN}
- Disclosure Delay: {DISCLOSURE_DELAY}

---
{format_table("Deterministic Mode", det_data)}
---
{format_table("Probabilistic Mode", prob_data)}
---
{format_table("Adaptive Mode", adapt_data)}
---
{format_table("Prob-Adaptive Mode", probadapt_data)}
"""
    with open(FINAL_REPORT, "w") as f:
        f.write(content)
    print(f"\n[*] Report generated: {FINAL_REPORT}")


def main() -> None:
    setup_directories()
    owner_proc = start_owner()

    try:
        total_uuids_needed = 4 * RUNS_PER_MODE
        uuids = get_uuids_from_owner(owner_proc, total_uuids_needed)

        det_uuids = uuids[:RUNS_PER_MODE]
        prob_uuids = uuids[RUNS_PER_MODE : RUNS_PER_MODE * 2]
        adapt_uuids = uuids[RUNS_PER_MODE * 2 : RUNS_PER_MODE * 3]
        probadapt_uuids = uuids[RUNS_PER_MODE * 3 :]

        det_raw_data: List[Dict[float, Dict[str, float]]] = []
        prob_raw_data: List[Dict[float, Dict[str, float]]] = []
        adapt_raw_data: List[Dict[float, Dict[str, float]]] = []
        probadapt_raw_data: List[Dict[float, Dict[str, float]]] = []

        print(
            f"\n[*] Starting Benchmark Suite ({RUNS_PER_MODE} runs per mode, {DURATION}s each)"
        )

        print("\n--- Phase 1: Deterministic Mode ---")
        for i, uid in enumerate(det_uuids):
            log_file = run_rcd_benchmark("deterministic", i + 1, uid)
            det_raw_data.append(parse_log_file(log_file))
            time.sleep(1)

        print("\n--- Phase 2: Probabilistic Mode ---")
        for i, uid in enumerate(prob_uuids):
            log_file = run_rcd_benchmark("probabilistic", i + 1, uid)
            prob_raw_data.append(parse_log_file(log_file))
            time.sleep(1)

        print("\n--- Phase 3: Adaptive Mode ---")
        for i, uid in enumerate(adapt_uuids):
            log_file = run_rcd_benchmark("adaptive", i + 1, uid)
            adapt_raw_data.append(parse_log_file(log_file))
            time.sleep(1)

        print("\n--- Phase 4: Prob-Adaptive Mode ---")
        for i, uid in enumerate(probadapt_uuids):
            log_file = run_rcd_benchmark("probadaptive", i + 1, uid)
            probadapt_raw_data.append(parse_log_file(log_file))
            time.sleep(1)

        print("\n[*] Aggregating data...")
        generate_markdown(
            aggregate_data(det_raw_data),
            aggregate_data(prob_raw_data),
            aggregate_data(adapt_raw_data),
            aggregate_data(probadapt_raw_data),
        )

    finally:
        print("[*] Stopping Owner Node...")
        owner_proc.terminate()
        try:
            owner_proc.wait(timeout=2)
        except:
            owner_proc.kill()


if __name__ == "__main__":
    main()
