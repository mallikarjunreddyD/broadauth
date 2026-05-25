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
    bi_peak: List[float] = []
    
    for k, v in data.items():
        losses.append(int(k.replace("%", "")))
        t_avg.append(v["avg_t_ms"])
        di_peak.append(v["peak_di"] * 100)
        batch_avg.append(v["avg_batch_size"])
        drops.append(v["security_drops"])
        successes.append(v["verified_batches"])
        bi_peak.append(v.get("peak_bi", 0.0))

    return losses, t_avg, di_peak, batch_avg, drops, successes, bi_peak


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

def plot_congestion_trigger(losses: List[int], di_peak: List[float], bi_peak: List[float]) -> None:
    plt.figure(figsize=(10, 6))
    w_disc = 0.30
    w_lat = 0.70
    ci_scores = [(w_disc * (d / 100.0)) + (w_lat * b) for d, b in zip(di_peak, bi_peak)]

    plt.plot(losses, ci_scores, marker="s", color="#9467bd", linewidth=2, markersize=8, label="Cumulative Score ($C_i$)")
    
    plt.axhline(
        y=0.75, 
        color="red", 
        linestyle="--", 
        linewidth=2,
        label="Safety Toggle Threshold (0.75)"
    )
    
    plt.fill_between(losses, 0, ci_scores, color="#9467bd", alpha=0.1)
    
    plt.title("Algorithmic Trigger: Congestion Score vs Safety Threshold", fontweight="bold")
    plt.xlabel("Network Packet Loss (%)")
    plt.ylabel("Congestion Score ($C_i$)")
    plt.ylim(0, 1.1)
    plt.grid(True, linestyle="--", alpha=0.5)
    plt.legend()
    plt.savefig(f"{OUTPUT_DIR}/congestion_trigger_score.png", bbox_inches="tight", dpi=300)
    plt.close()
    
def plot_slot_duration_bar(losses: List[int], t_avg: List[float]) -> None:
    plt.figure(figsize=(10, 6))
    
    bars = plt.bar(losses, t_avg, color="#ff7f0e", alpha=0.8, width=6)
    
    plt.axhline(
        y=1000, color="gray", linestyle=":", alpha=0.7, label="Baseline Cadence (1000ms)"
    )
    
    for bar in bars:
        yval = bar.get_height()
        plt.text(bar.get_x() + bar.get_width()/2, yval + 100, f'{int(yval)}ms', ha='center', va='bottom', fontsize=9)

    plt.title("Adaptive Cadence: Average Slot Duration per Phase", fontweight="bold")
    plt.xlabel("Network Packet Loss (%)")
    plt.ylabel("Average Slot Duration ($T_i$) in ms")
    plt.ylim(0, max(t_avg) + 1500)  # Give headroom for the text labels
    plt.grid(axis="y", linestyle="--", alpha=0.5)
    plt.legend()
    plt.savefig(f"{OUTPUT_DIR}/cadence_slot_duration_bar.png", bbox_inches="tight", dpi=300)
    plt.close()

if __name__ == "__main__":
    print("[*] Generating Sweep Charts...")
    l, t, d, b, dr, s, bi = load_data()
    plot_elasticity(l, t)
    plot_survivability(l, d)
    plot_compression(l, b)
    plot_security(l, dr, s)
    plot_congestion_trigger(l, d, bi)
    plot_slot_duration_bar(l, t)
    print(f"[*] Success! Charts saved to {OUTPUT_DIR}/")
