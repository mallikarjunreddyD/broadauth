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
DURATION = 90  # Seconds per phase
LOSS_RATES = [0, 10, 20, 40, 60, 80, 100]

# Binaries & Shared Arguments
RCD_BIN = "./bin/rcd"
OWNER_BIN = "./bin/owner"
ETH_URL = "http://0.0.0.0:8545"

# !!! ENSURE THIS IS YOUR CORRECT FORGE DEPLOYMENT ADDRESS !!!
CONTRACT_ADDR = "0x5FbDB2315678afecb367f032d93F642f64180aa3"

OWNER_ADDR = "0.0.0.0:10102"
OWNER_PORT = "10102"
OWNER_PRIV_KEY = "59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d"
CM_ADDR = "0.0.0.0:10101"
HASHCHAIN_LEN = "1024"
DISCLOSURE_DELAY = "2"

# Regex Parsers
RE_DI = re.compile(r"D_i \(Queue\): ([\d\.]+)")
RE_TOGGLE = re.compile(r"Scaling T_i: \d+ms -> (\d+)ms")
RE_BATCH = re.compile(r"Batch of (\d+) packets")
RE_DROP = re.compile(r"\[SECURITY\] Dropped")
RE_SUCCESS = re.compile(r"\[SUCCESS\] Prob-Adaptive Batch Verification")


def setup_directories():
    if not os.path.exists(SWEEP_DIR):
        os.makedirs(SWEEP_DIR)
    # Ensure network is clean before starting
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
    while time.time() - start_time < 20:
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
    """
    Applies tc rules ONLY to UDP traffic (protocol 17).
    TCP traffic (Anvil RPC) remains unthrottled to prevent blockchain disconnects!
    """
    subprocess.run("tc qdisc del dev lo root", shell=True, stderr=subprocess.DEVNULL)
    if loss > 0:
        # 1. Create a priority queue at the root
        subprocess.run(
            "tc qdisc add dev lo root handle 1: prio", shell=True, check=True
        )
        # 2. Add the netem delay/drop rule to band 1 (flowid 1:1)
        subprocess.run(
            f"tc qdisc add dev lo parent 1:1 handle 10: netem delay 100ms drop {loss}%",
            shell=True,
            check=True,
        )
        # 3. Filter UDP traffic (protocol 17) to go through the throttled band 1
        subprocess.run(
            "tc filter add dev lo protocol ip parent 1:0 u32 match ip protocol 17 0xff flowid 1:1",
            shell=True,
            check=True,
        )
        print(f"[*] Applied Selective UDP Throttle: 100ms delay, {loss}% packet drop")
    else:
        print("[*] Network is clean (0% drop)")


def parse_sweep_log(filepath: str) -> Dict:
    current_t = 1000
    t_history = []
    di_history = []
    batch_sizes = []
    drops = 0
    successes = 0

    with open(filepath, "r") as f:
        for line in f:
            di_match = RE_DI.search(line)
            if di_match:
                di_history.append(float(di_match.group(1)))
                t_history.append(current_t)

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
        uuids = get_uuids_from_owner(owner_proc, len(LOSS_RATES))

        if len(uuids) < len(LOSS_RATES):
            print("[!] Not enough UUIDs generated. Exiting.")
            sys.exit(1)

        for i, loss in enumerate(LOSS_RATES):
            uid = uuids[i]
            print(f"\n========================================")
            print(f"      PHASE: {loss}% PACKET LOSS")
            print(f"========================================")

            # Ensure network is clean for RCD boot-up
            subprocess.run(
                "tc qdisc del dev lo root", shell=True, stderr=subprocess.DEVNULL
            )

            log_file = f"{SWEEP_DIR}/probadaptive_loss_{loss}.log"
            print(f"[*] Booting RCD with UUID: {uid}")

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
                    ],
                    stdout=f,
                    stderr=subprocess.STDOUT,
                )

                print("[*] Waiting 5 seconds for RCD to fetch initial hashchain...")
                time.sleep(5)

                # Now apply the UDP chaos!
                apply_network_chaos(loss)
                print(f"[*] Running protocol simulation for {DURATION - 5}s...")

                try:
                    time.sleep(DURATION - 5)
                except KeyboardInterrupt:
                    rcd_proc.terminate()
                    raise

                rcd_proc.terminate()
                rcd_proc.wait()

            metrics = parse_sweep_log(log_file)
            results[f"{loss}%"] = metrics
            print(f"  -> Avg Slot Duration: {metrics['avg_t_ms']:.0f}ms")
            print(f"  -> Peak Queue Pressure: {metrics['peak_di']:.2f}")
            print(f"  -> Avg Batch Size: {metrics['avg_batch_size']:.1f} packets")

        subprocess.run(
            "tc qdisc del dev lo root", shell=True, stderr=subprocess.DEVNULL
        )
        print("\n[*] Network restored to normal.")

        with open(RESULTS_FILE, "w") as f:
            json.dump(results, f, indent=4)
        print(f"[*] Sweep completed. Data saved to {RESULTS_FILE}")

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
