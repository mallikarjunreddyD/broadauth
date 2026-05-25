import subprocess
import time
import re
import os
import sys
import json
import threading
from typing import List, Dict

# --- CONFIGURATION ---
SWEEP_DIR = "benchmarks/sweep"
RESULTS_FILE = f"{SWEEP_DIR}/sweep_results.json"
DURATION = 120 
LOSS_RATES = [0, 10, 20, 30, 40, 50, 60, 70, 80, 90, 100] 
ITERATIONS = 10 

# Binaries & Shared Arguments
RCD_BIN = "./bin/rcd"
OWNER_BIN = "./bin/owner"
ETH_URL = "http://0.0.0.0:8545"
CONTRACT_ADDR = "0x5FbDB2315678afecb367f032d93F642f64180aa3"

OWNER_ADDR = "0.0.0.0:10102"
OWNER_PORT = "10102"
OWNER_PRIV_KEY = "59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d"
CM_ADDR = "0.0.0.0:10101"
HASHCHAIN_LEN = "1024"
DISCLOSURE_DELAY = "2"

# Regex Parsers
RE_DI = re.compile(r"D_i \(Queue\): ([\d\.]+)")
RE_BI = re.compile(r"B_i \(Latency\): ([\d\.]+)")
RE_TOGGLE = re.compile(r"Scaling T_i: \d+ms -> (\d+)ms")
RE_BATCH = re.compile(r"Batch of (\d+) packets")
RE_DROP = re.compile(r"\[SECURITY\] Dropped")
RE_SUCCESS = re.compile(r"\[SUCCESS\] Prob-Adaptive Batch Verification")


def setup_directories():
    if not os.path.exists(SWEEP_DIR):
        os.makedirs(SWEEP_DIR)
    subprocess.run("tc qdisc del dev lo root", shell=True, stderr=subprocess.DEVNULL)


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
        sys.exit(1)
    return proc


def get_uuids_from_owner(owner_proc: subprocess.Popen[str], count: int) -> List[str]:
    print(f"[*] Waiting for {count} UUIDs from Owner (Make sure CM is running!)...")
    uuids: List[str] = []
    owner_log_file = open(f"{SWEEP_DIR}/owner.log", "w")

    start_time = time.time()
    # Increased timeout to allow generating 70 UUIDs safely
    while time.time() - start_time < 60:
        if owner_proc.stdout is None:
            break
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

    threading.Thread(target=drain_owner_log, daemon=True).start()

    if len(uuids) < count:
        print(f"[!] Warning: Only captured {len(uuids)} UUIDs. Expected {count}.")
    else:
        print(f"[*] Successfully captured {len(uuids)} UUIDs.")
    return uuids


def apply_network_chaos(loss: int):
    subprocess.run("tc qdisc del dev lo root", shell=True, stderr=subprocess.DEVNULL)
    if loss > 0:
        subprocess.run(
            "tc qdisc add dev lo root handle 1: prio", shell=True, check=True
        )
        tc_cmd = f"tc qdisc add dev lo parent 1:1 handle 10: netem drop {loss}%"
        subprocess.run(tc_cmd, shell=True, check=True)
        subprocess.run(
            "tc filter add dev lo protocol ip parent 1:0 u32 match ip protocol 17 0xff flowid 1:1",
            shell=True,
            check=True,
        )
        print(
            f"    [+] Applied Selective UDP Throttle: Pure {loss}% packet drop pipeline"
        )
    else:
        print("    [+] Network is clean (0% drop)")


def parse_sweep_log(filepath: str) -> Dict:
    current_t = 1000
    t_history = []
    di_history = []
    bi_history = []
    batch_sizes = []
    drops = 0
    successes = 0

    with open(filepath, "r") as f:
        for line in f:
            di_match = RE_DI.search(line)
            if di_match:
                di_history.append(float(di_match.group(1)))
                t_history.append(current_t)

            bi_match = RE_BI.search(line)
            if bi_match:
                bi_history.append(float(bi_match.group(1)))

            toggle_match = RE_TOGGLE.search(line)
            if toggle_match:
                current_t = int(toggle_match.group(1))

            batch_match = RE_BATCH.search(line)
            if batch_match:
                batch_sizes.append(int(batch_match.group(1)))

            if RE_DROP.search(line):
                drops += 1
            if RE_SUCCESS.search(line):
                successes += 1

    return {
        "avg_t_ms": sum(t_history) / len(t_history) if t_history else 1000,
        "peak_di": max(di_history) if di_history else 0.0,
        "peak_bi": max(bi_history) if bi_history else 0.0,
        "avg_batch_size": sum(batch_sizes) / len(batch_sizes) if batch_sizes else 0,
        "security_drops": drops,
        "verified_batches": successes,
    }


def main():
    if os.geteuid() != 0:
        print(
            "[!] ERROR: This script modifies network interfaces and must be run as root (sudo)."
        )
        sys.exit(1)

    setup_directories()
    results = {}
    owner_proc = start_owner()

    try:
        total_runs = len(LOSS_RATES) * ITERATIONS
        uuids = get_uuids_from_owner(owner_proc, total_runs)

        if len(uuids) < total_runs:
            print("[!] Not enough UUIDs generated. Exiting.")
            sys.exit(1)

        uuid_index = 0

        for loss in LOSS_RATES:
            print(f"\n========================================")
            print(f"  PHASE: {loss}% PACKET LOSS ({ITERATIONS} Trials)")
            print(f"========================================")

            phase_metrics = {
                "avg_t_ms": [],
                "peak_di": [],
                "peak_bi": [],
                "avg_batch_size": [],
                "security_drops": [],
                "verified_batches": [],
            }

            for run in range(ITERATIONS):
                uid = uuids[uuid_index]
                uuid_index += 1

                print(f"  [*] Trial {run + 1}/{ITERATIONS} (UUID: {uid})")
                subprocess.run(
                    "tc qdisc del dev lo root", shell=True, stderr=subprocess.DEVNULL
                )

                log_file = f"{SWEEP_DIR}/probadaptive_loss_{loss}_run_{run + 1}.log"

                with open(log_file, "w") as f:
                    rcd_proc = subprocess.Popen(
                        [
                            RCD_BIN,
                            "-contract",
                            CONTRACT_ADDR,
                            "-eth-url",
                            ETH_URL,
                            "-hashchain-len",
                            HASHCHAIN_LEN,
                            "-owner-addr",
                            OWNER_ADDR,
                            "-uuid",
                            uid,
                            "-mode",
                            "probadaptive",
                            "-t-min",
                            "1000",
                            "-t-max",
                            "8000",
                            "-disclosure-delay",
                            DISCLOSURE_DELAY,
                            "-bench",
                        ],
                        stdout=f,
                        stderr=subprocess.STDOUT,
                    )

                    time.sleep(5)  # Let RCD fetch hashchain
                    apply_network_chaos(loss)

                    try:
                        time.sleep(DURATION - 5)
                    except KeyboardInterrupt:
                        rcd_proc.terminate()
                        raise

                    rcd_proc.terminate()
                    rcd_proc.wait()

                # Parse the individual run
                metrics = parse_sweep_log(log_file)
                for k in phase_metrics.keys():
                    phase_metrics[k].append(metrics[k])

            # Calculate mathematical averages for the phase
            averaged_metrics = {
                k: sum(v) / len(v) if len(v) > 0 else 0
                for k, v in phase_metrics.items()
            }
            results[f"{loss}%"] = averaged_metrics

            # Print aggregated phase summary
            print(f"\n  [AGGREGATE SUMMARY: {loss}% LOSS]")
            print(f"  -> Avg Slot Duration: {averaged_metrics['avg_t_ms']:.0f}ms")
            print(f"  -> Avg Peak Queue Pressure: {averaged_metrics['peak_di']:.2f}")
            print(f"  -> Avg Peak Latency: {averaged_metrics['peak_bi']:.2f}")
            print(
                f"  -> Avg Batch Size: {averaged_metrics['avg_batch_size']:.1f} packets"
            )
            print(
                f"  -> Avg Authenticated Batches: {averaged_metrics['verified_batches']:.1f}"
            )
            print(
                f"  -> Avg Prevented Forgeries: {averaged_metrics['security_drops']:.1f}"
            )

        subprocess.run(
            "tc qdisc del dev lo root", shell=True, stderr=subprocess.DEVNULL
        )
        print("\n[*] Network restored to normal.")

        with open(RESULTS_FILE, "w") as f:
            json.dump(results, f, indent=4)
        print(f"[*] Full sweep completed. Aggregated data saved to {RESULTS_FILE}")

    finally:
        print("[*] Stopping Owner Node...")
        owner_proc.terminate()
        try:
            owner_proc.wait(timeout=2)
        except:
            owner_proc.kill()
        subprocess.run(
            "tc qdisc del dev lo root", shell=True, stderr=subprocess.DEVNULL
        )


if __name__ == "__main__":
    main()
