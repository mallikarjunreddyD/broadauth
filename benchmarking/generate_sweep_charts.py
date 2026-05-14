import json
from typing import List, Tuple, Dict, Any
import matplotlib.pyplot as plt 
import numpy as np 
import os

SWEEP_DATA = "benchmarks/sweep/sweep_results.json"
OUTPUT_DIR = "plots/sweep"

if not os.path.exists(OUTPUT_DIR):
    os.makedirs(OUTPUT_DIR)


def load_data() -> (
    Tuple[List[int], List[float], List[float], List[float], List[int], List[int]]
):
    with open(SWEEP_DATA, "r") as f:
        data: Dict[str, Any] = json.load(f)

    losses: List[int] = []
    t_avg: List[float] = []
    di_peak: List[float] = []
    batch_avg: List[float] = []
    drops: List[int] = []
    successes: List[int] = []

    for k, v in data.items():
        losses.append(int(k.replace("%", "")))
        t_avg.append(v["avg_t_ms"])
        di_peak.append(v["peak_di"] * 100)  # Convert to percentage
        batch_avg.append(v["avg_batch_size"])
        drops.append(v["security_drops"])
        successes.append(v["verified_batches"])

    return losses, t_avg, di_peak, batch_avg, drops, successes


def plot_elasticity(losses: List[int], t_avg: List[float]) -> None:  # type: ignore
    plt.figure(figsize=(10, 6))
    plt.plot(losses, t_avg, marker="D", color="#d62728", linewidth=2, markersize=8)
    plt.axhline(
        y=8000,
        color="gray",
        linestyle="--",
        alpha=0.7,
        label="Cryptographic Ceiling (T_max)",
    )
    plt.axhline(
        y=1000, color="gray", linestyle=":", alpha=0.7, label="Baseline (T_min)"
    )
    plt.title(
        "Protocol Elasticity: Average Slot Duration vs Network Degradation",
        fontweight="bold",
    )
    plt.xlabel("Network Packet Loss (%)")
    plt.ylabel("Average Slot Duration (ms)")
    plt.grid(True, linestyle="--", alpha=0.5)
    plt.legend()
    plt.savefig(f"{OUTPUT_DIR}/elasticity_t_scale.png", bbox_inches="tight", dpi=300)
    plt.close()


def plot_survivability(losses: List[int], di_peak: List[float]) -> None:  # type: ignore
    plt.figure(figsize=(10, 6))
    plt.plot(losses, di_peak, marker="o", color="#1f77b4", linewidth=2, markersize=8)
    plt.axhline(
        y=100,
        color="red",
        linestyle="--",
        alpha=0.5,
        label="Buffer Overflow (Crash Zone)",
    )
    plt.fill_between(losses, di_peak, color="#1f77b4", alpha=0.1)
    plt.title("System Survivability: Peak Queue Pressure ($D_i$)", fontweight="bold")
    plt.xlabel("Network Packet Loss (%)")
    plt.ylabel("Peak Queue Utilization (%)")
    plt.grid(True, linestyle="--", alpha=0.5)
    plt.legend()
    plt.savefig(f"{OUTPUT_DIR}/survivability_queue.png", bbox_inches="tight", dpi=300)
    plt.close()


def plot_compression(losses: List[int], batch_avg: List[float]) -> None:  # type: ignore
    plt.figure(figsize=(10, 6))
    plt.bar(losses, batch_avg, color="#2ca02c", alpha=0.8, width=6)
    plt.title(
        "Cryptographic Compression: Packets Compressed per Broadcast", fontweight="bold"
    )
    plt.xlabel("Network Packet Loss (%)")
    plt.ylabel("Average Packets in Bloom Filter")
    plt.grid(axis="y", linestyle="--", alpha=0.5)
    plt.savefig(
        f"{OUTPUT_DIR}/compression_batch_size.png", bbox_inches="tight", dpi=300
    )
    plt.close()


def plot_security(losses: List[int], drops: List[int], successes: List[int]) -> None:  # type: ignore
    plt.figure(figsize=(10, 6))
    x = np.arange(len(losses))
    width = 0.35

    plt.bar(
        x - width / 2, successes, width, label="Verified & Accepted", color="#2ca02c"
    )
    plt.bar(
        x + width / 2, drops, width, label="Dropped (Arrived Late)", color="#d62728"
    )

    plt.title(
        "Security Integrity: Safe Batches vs. Forgery Rejections", fontweight="bold"
    )
    plt.xlabel("Network Packet Loss (%)")
    plt.ylabel("Count")
    plt.xticks(x, [f"{l}%" for l in losses])
    plt.legend()
    plt.grid(axis="y", linestyle="--", alpha=0.5)
    plt.savefig(f"{OUTPUT_DIR}/security_integrity.png", bbox_inches="tight", dpi=300)
    plt.close()


if __name__ == "__main__":
    print("[*] Generating Sweep Charts...")
    l, t, d, b, dr, s = load_data()
    plot_elasticity(l, t)
    plot_survivability(l, d)
    plot_compression(l, b)
    plot_security(l, dr, s)
    print(f"[*] Success! Charts saved to {OUTPUT_DIR}/")
