import glob
import json
import os
from statistics import mean
from typing import Dict, List, Optional

import matplotlib.pyplot as plt  # type: ignore
import numpy as np  # type: ignore

from benchmark import (  # type: ignore
    BANDWIDTH_CAPS_KBPS,
    BENCH_DIR,
    EXPERIMENT_CONFIGS,
    LIFESPAN_RATES,
    parse_log_file,
)

output_dir = "plots"
if not os.path.exists(output_dir):
    os.makedirs(output_dir)


# ---------------------------------------------------------------------------
# Shared plotting helpers
# ---------------------------------------------------------------------------


def save_line_chart(
    filename: str,
    series: Dict[str, List[float]],
    x: List[float],
    title: str,
    y_label: str,
    x_label: str = "Time (s)",
    log_scale: bool = False,
) -> None:
    plt.figure(figsize=(10, 6))  # type: ignore
    colors = ["#1f77b4", "#ff7f0e", "#2ca02c", "#d62728"]
    markers = ["o", "s", "^", "d"]
    for i, (label, y) in enumerate(series.items()):
        plt.plot(x, y, label=label, color=colors[i % len(colors)], marker=markers[i % len(markers)], linewidth=2)  # type: ignore

    plt.title(title, fontsize=14, fontweight="bold", pad=15)  # type: ignore
    plt.xlabel(x_label, fontsize=12)  # type: ignore
    plt.ylabel(y_label, fontsize=12)  # type: ignore
    plt.grid(True, linestyle="--", alpha=0.5)  # type: ignore
    plt.legend(fontsize=10)  # type: ignore
    if log_scale:
        plt.yscale("log")  # type: ignore

    path = os.path.join(output_dir, filename)
    plt.savefig(path, dpi=300, bbox_inches="tight")  # type: ignore
    plt.close()  # type: ignore
    print(f"Saved: {path}")


def save_bar_chart(
    filename: str,
    series: Dict[str, List[float]],
    x_labels: List[str],
    title: str,
    y_label: str,
    x_label: str = "",
) -> None:
    plt.figure(figsize=(10, 6))  # type: ignore
    n = len(series)
    width = 0.8 / max(n, 1)
    x_indices = np.arange(len(x_labels))  # type: ignore
    colors = ["#1f77b4", "#ff7f0e", "#2ca02c", "#d62728"]

    for i, (label, y) in enumerate(series.items()):
        offset = (i - (n - 1) / 2) * width
        plt.bar(x_indices + offset, y, width=width, label=label, color=colors[i % len(colors)], alpha=0.85)  # type: ignore

    plt.xticks(x_indices, x_labels)  # type: ignore
    plt.title(title, fontsize=14, fontweight="bold", pad=15)  # type: ignore
    plt.xlabel(x_label, fontsize=12)  # type: ignore
    plt.ylabel(y_label, fontsize=12)  # type: ignore
    plt.grid(True, linestyle="--", alpha=0.5, axis="y")  # type: ignore
    plt.legend(fontsize=10)  # type: ignore

    path = os.path.join(output_dir, filename)
    plt.savefig(path, dpi=300, bbox_inches="tight")  # type: ignore
    plt.close()  # type: ignore
    print(f"Saved: {path}")


def load_and_average_runs(pattern: str) -> Dict[float, Dict[str, float]]:
    """Average parse_log_file(...) across every log file matching a glob
    pattern under BENCH_DIR (real parsed data, not hardcoded arrays)."""
    files = sorted(glob.glob(os.path.join(BENCH_DIR, pattern)))
    if not files:
        return {}
    runs = [parse_log_file(f) for f in files]
    all_times: List[float] = sorted(set().union(*[r.keys() for r in runs]))
    out: Dict[float, Dict[str, float]] = {}
    for t in all_times:
        vals = [r[t] for r in runs if t in r]
        if not vals:
            continue
        out[t] = {k: mean(v[k] for v in vals) for k in vals[0].keys()}
    return out


def load_json(filename: str) -> Optional[object]:
    path = os.path.join(BENCH_DIR, filename)
    if not os.path.exists(path):
        print(f"[!] Missing {path}, skipping charts that depend on it.")
        return None
    with open(path) as f:
        return json.load(f)


# ---------------------------------------------------------------------------
# 1. Baseline deterministic vs. probabilistic charts (from real logs)
# ---------------------------------------------------------------------------

det_data = load_and_average_runs("run_*_deterministic_*.log")
prob_data = load_and_average_runs("run_*_probabilistic_*.log")

if det_data and prob_data:
    times = sorted(set(det_data.keys()) & set(prob_data.keys()))
    if times:
        save_line_chart(
            "throughput.png",
            {
                "Deterministic": [det_data[t]["msg_rate"] for t in times],
                "Probabilistic": [prob_data[t]["msg_rate"] for t in times],
            },
            times,
            "Protocol Throughput (Messages per Second)",
            "Messages / sec",
        )
        save_line_chart(
            "bandwidth.png",
            {
                "Deterministic": [det_data[t]["tx_rate"] for t in times],
                "Probabilistic": [prob_data[t]["tx_rate"] for t in times],
            },
            times,
            "Bandwidth Usage (Tx Rate)",
            "Bytes / sec",
        )
        save_line_chart(
            "overhead.png",
            {
                "Deterministic": [det_data[t]["overhead"] for t in times],
                "Probabilistic": [prob_data[t]["overhead"] for t in times],
            },
            times,
            "Cumulative Protocol Overhead (Bytes)",
            "Bytes (Log Scale)",
            log_scale=True,
        )
        save_line_chart(
            "memory.png",
            {
                "Deterministic": [det_data[t]["mem"] for t in times],
                "Probabilistic": [prob_data[t]["mem"] for t in times],
            },
            times,
            "Memory Usage (Heap Allocation)",
            "Megabytes (MB)",
        )
        save_line_chart(
            "broadcast_latency.png",
            {
                "Deterministic": [det_data[t]["bcast_lat"] for t in times],
                "Probabilistic": [prob_data[t]["bcast_lat"] for t in times],
            },
            times,
            "Average Broadcast Latency (System Call)",
            "Microseconds (µs)",
        )

        def safe_div(n: float, d: float) -> float:
            return n / d if d > 0 else 0.0

        save_line_chart(
            "efficiency.png",
            {
                "Deterministic": [safe_div(det_data[t]["msg_rate"], det_data[t]["tx_rate"]) * 1000 for t in times],
                "Probabilistic": [safe_div(prob_data[t]["msg_rate"], prob_data[t]["tx_rate"]) * 1000 for t in times],
            },
            times,
            "Protocol Efficiency (Messages delivered per KB)",
            "Messages / KB",
        )
else:
    print("[!] No baseline run_*_deterministic_*/run_*_probabilistic_*.log files found under "
          f"{BENCH_DIR}/ - run `python benchmark.py --experiment baseline` first.")


# ---------------------------------------------------------------------------
# 2. Chart 1: keychain lifespan vs. injected message rate
# ---------------------------------------------------------------------------

def _get_series(results: Dict[str, Dict[str, float]], label: str, keys: List) -> List[float]:
    d = results.get(label, {})
    return [d.get(str(k), d.get(k, 0.0)) for k in keys]


lifespan_results = load_json("lifespan_results.json")
if lifespan_results:
    rates = LIFESPAN_RATES
    series = {
        "Deterministic (baseline)": _get_series(lifespan_results, "det-baseline", rates),  # type: ignore
        "Deterministic (adaptive)": _get_series(lifespan_results, "det-adaptive", rates),  # type: ignore
        "Probabilistic (baseline)": _get_series(lifespan_results, "prob-baseline", rates),  # type: ignore
        "Probabilistic (adaptive)": _get_series(lifespan_results, "prob-adaptive", rates),  # type: ignore
    }
    save_line_chart(
        "keychain_lifespan.png",
        series,
        rates,
        "Keychain Lifespan vs. Injected Message Rate (Det/Prob x Baseline/Adaptive)",
        "Time to first exhaustion (s)",
        x_label="Injected message rate (packets/sec)",
    )


# ---------------------------------------------------------------------------
# 3. Chart 2: throughput vs. emulated bandwidth
# ---------------------------------------------------------------------------

bandwidth_results = load_json("bandwidth_results.json")
if bandwidth_results:
    caps = BANDWIDTH_CAPS_KBPS
    cap_labels = [f"{c}" if c > 0 else "uncapped" for c in caps]
    series = {
        "Deterministic (baseline)": _get_series(bandwidth_results, "det-baseline", caps),  # type: ignore
        "Deterministic (adaptive)": _get_series(bandwidth_results, "det-adaptive", caps),  # type: ignore
        "Probabilistic (baseline)": _get_series(bandwidth_results, "prob-baseline", caps),  # type: ignore
        "Probabilistic (adaptive)": _get_series(bandwidth_results, "prob-adaptive", caps),  # type: ignore
    }
    plt.figure(figsize=(10, 6))  # type: ignore
    colors = ["#1f77b4", "#aec7e8", "#ff7f0e", "#ffbb78"]
    markers = ["o", "o", "s", "s"]
    for i, (label, y) in enumerate(series.items()):
        linestyle = "--" if "baseline" in label.lower() else "-"
        plt.plot(range(len(caps)), y, label=label, color=colors[i], marker=markers[i], linestyle=linestyle, linewidth=2)  # type: ignore
    plt.xticks(range(len(caps)), cap_labels)  # type: ignore
    plt.title("Authenticated Data Throughput vs. Emulated Bandwidth (Det/Prob x Baseline/Adaptive)", fontsize=14, fontweight="bold", pad=15)  # type: ignore
    plt.xlabel("Bandwidth cap (kbit/s)", fontsize=12)  # type: ignore
    plt.ylabel("Avg Tx Rate (Bytes/sec)", fontsize=12)  # type: ignore
    plt.grid(True, linestyle="--", alpha=0.5)  # type: ignore
    plt.legend(fontsize=10)  # type: ignore
    path = os.path.join(output_dir, "throughput_vs_bandwidth.png")
    plt.savefig(path, dpi=300, bbox_inches="tight")  # type: ignore
    plt.close()  # type: ignore
    print(f"Saved: {path}")


# ---------------------------------------------------------------------------
# 4. Step-response: T_cur and queue utilization over time under an induced
#    stress window. This is the one chart that shows the controller actually
#    stretching under stress and recovering, not just bounded steady state.
# ---------------------------------------------------------------------------

step_ticks = load_json("step_response_ticks.json")
if step_ticks:
    ticks = step_ticks  # type: ignore
    t = [tick["t"] for tick in ticks]  # type: ignore
    t_cur = [tick["t_cur_ms"] for tick in ticks]  # type: ignore
    u_cur = [tick["u_cur"] for tick in ticks]  # type: ignore

    fig, ax1 = plt.subplots(figsize=(10, 6))  # type: ignore
    ax1.plot(t, t_cur, color="#1f77b4", marker="o", markersize=3, linewidth=2, label="T_cur (ms)")  # type: ignore
    ax1.set_xlabel("Time (s)", fontsize=12)  # type: ignore
    ax1.set_ylabel("Slot duration T_cur (ms)", color="#1f77b4", fontsize=12)  # type: ignore
    ax1.tick_params(axis="y", labelcolor="#1f77b4")  # type: ignore
    ax1.grid(True, linestyle="--", alpha=0.5)  # type: ignore

    ax2 = ax1.twinx()  # type: ignore
    ax2.plot(t, u_cur, color="#d62728", marker="s", markersize=3, linewidth=2, label="U_cur (queue utilization)")  # type: ignore
    ax2.set_ylabel("Queue utilization U_cur", color="#d62728", fontsize=12)  # type: ignore
    ax2.tick_params(axis="y", labelcolor="#d62728")  # type: ignore
    ax2.set_ylim(0, 1.05)  # type: ignore

    plt.title("Step Response: T_cur and Queue Utilization Under Induced Stress", fontsize=14, fontweight="bold", pad=15)  # type: ignore
    fig.tight_layout()  # type: ignore
    path = os.path.join(output_dir, "step_response.png")
    plt.savefig(path, dpi=300)  # type: ignore
    plt.close()  # type: ignore
    print(f"Saved: {path}")


# ---------------------------------------------------------------------------
# 5. Receiver-side chart: verified vs. premature-disclosure drops across the
#    bandwidth sweep (reuses the bandwidth experiment's logs, no new runs).
# ---------------------------------------------------------------------------

if bandwidth_results:
    caps = BANDWIDTH_CAPS_KBPS
    # Receiver-side wall-clock rejections only exist under adaptive mode
    # (non-adaptive uses the old slot-count check and never touches
    # MessagesDroppedPremature), so this pulls specifically from the
    # prob-adaptive runs of the bandwidth sweep.
    verified: List[float] = []
    dropped: List[float] = []
    for c in caps:
        files = sorted(glob.glob(os.path.join(BENCH_DIR, f"run_*_bandwidth_prob-adaptive_{c}kbps_*.log")))
        if not files:
            verified.append(0.0)
            dropped.append(0.0)
            continue
        data = parse_log_file(files[0])
        if not data:
            verified.append(0.0)
            dropped.append(0.0)
            continue
        last_t = max(data.keys())
        verified.append(data[last_t]["verified"])
        dropped.append(data[last_t]["dropped_premature"])

    labels = [f"{c}" if c > 0 else "uncapped" for c in caps]
    save_bar_chart(
        "receiver_verification_outcomes.png",
        {"Verified": verified, "Dropped (premature disclosure)": dropped},
        labels,
        "Receiver Verification Outcomes vs. Emulated Bandwidth (Probabilistic, Adaptive)",
        "Messages",
        x_label="Bandwidth cap (kbit/s)",
    )
