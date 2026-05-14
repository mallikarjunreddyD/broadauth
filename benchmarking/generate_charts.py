import matplotlib.pyplot as plt
import numpy as np
import os
from typing import List, Dict, Tuple


# --- DYNAMIC DATA PARSER ---
def parse_markdown_report(
    filepath: str,
) -> Tuple[
    Dict[str, List[float]],
    Dict[str, List[float]],
    Dict[str, List[float]],
    Dict[str, List[float]],
]:
    data: Dict[str, Dict[str, List[float]]] = {
        "det": {
            "time": [],
            "tx": [],
            "rx": [],
            "msg": [],
            "mem": [],
            "overhead": [],
            "bcast": [],
        },
        "prob": {
            "time": [],
            "tx": [],
            "rx": [],
            "msg": [],
            "mem": [],
            "overhead": [],
            "bcast": [],
        },
        "adapt": {
            "time": [],
            "tx": [],
            "rx": [],
            "msg": [],
            "mem": [],
            "overhead": [],
            "bcast": [],
        },
        "probadapt": {
            "time": [],
            "tx": [],
            "rx": [],
            "msg": [],
            "mem": [],
            "overhead": [],
            "bcast": [],
        },
    }

    current_mode = None
    if not os.path.exists(filepath):
        print(f"Error: {filepath} not found. Run benchmark.py first.")
        exit(1)

    with open(filepath, "r") as f:
        for line in f:
            if "### Deterministic" in line:
                current_mode = "det"
            elif "### Probabilistic" in line:
                current_mode = "prob"
            elif "### Adaptive" in line:
                current_mode = "adapt"
            elif "### Prob-Adaptive" in line:
                current_mode = "probadapt"
            elif (
                line.startswith("| ")
                and current_mode
                and "Time" not in line
                and "---" not in line
            ):
                parts = [p.strip() for p in line.split("|") if p.strip()]
                if len(parts) >= 9:
                    data[current_mode]["time"].append(float(parts[0]))
                    data[current_mode]["tx"].append(float(parts[1]))
                    data[current_mode]["rx"].append(float(parts[2]))
                    data[current_mode]["msg"].append(float(parts[3]))
                    data[current_mode]["mem"].append(float(parts[4]))
                    data[current_mode]["overhead"].append(float(parts[5]))
                    data[current_mode]["bcast"].append(float(parts[8]))
    return data["det"], data["prob"], data["adapt"], data["probadapt"]


output_dir = "plots"
if not os.path.exists(output_dir):
    os.makedirs(output_dir)


# Helper function
def save_chart(
    filename: str,
    x_data: List[float],
    y_data_dict: Dict[str, List[float]],
    title: str,
    y_label: str,
    log_scale: bool = False,
    is_line: bool = False,
) -> None:
    plt.figure(figsize=(12, 7))

    colors = {
        "Deterministic": "#1f77b4",
        "Probabilistic": "#ff7f0e",
        "Adaptive": "#2ca02c",
        "Prob-Adaptive": "#d62728",
    }
    markers = {
        "Deterministic": "o",
        "Probabilistic": "s",
        "Adaptive": "^",
        "Prob-Adaptive": "D",
    }

    x_indices = np.array(x_data)

    if is_line:
        for label, y_vals in y_data_dict.items():
            plt.plot(
                x_indices[: len(y_vals)],
                y_vals,
                label=label,
                color=colors[label],
                marker=markers[label],
                linewidth=2,
                alpha=0.9,
            )
    else:
        bar_width = 1.0
        offsets = [-1.5 * bar_width, -0.5 * bar_width, 0.5 * bar_width, 1.5 * bar_width]
        for i, (label, y_vals) in enumerate(y_data_dict.items()):
            plt.bar(
                x_indices[: len(y_vals)] + offsets[i],
                y_vals,
                width=bar_width,
                label=label,
                color=colors[label],
                alpha=0.85,
            )

    plt.title(title, fontsize=14, fontweight="bold", pad=15)
    plt.xlabel("Time (s)", fontsize=12)
    plt.ylabel(y_label, fontsize=12)
    plt.grid(True, linestyle="--", alpha=0.5)
    plt.legend(fontsize=10)

    if log_scale:
        plt.yscale("log")

    path = os.path.join(output_dir, filename)
    plt.savefig(path, dpi=300, bbox_inches="tight")
    plt.close()
    print(f"Saved: {path}")


# --- EXECUTION ---
det, prob, adapt, probadapt = parse_markdown_report("avg_benchmarks.md")
time_steps = det["time"]

save_chart(
    "throughput.png",
    time_steps,
    {
        "Deterministic": det["msg"],
        "Probabilistic": prob["msg"],
        "Adaptive": adapt["msg"],
        "Prob-Adaptive": probadapt["msg"],
    },
    "Protocol Throughput",
    "Messages / sec",
)
save_chart(
    "bandwidth.png",
    time_steps,
    {
        "Deterministic": det["tx"],
        "Probabilistic": prob["tx"],
        "Adaptive": adapt["tx"],
        "Prob-Adaptive": probadapt["tx"],
    },
    "Bandwidth Usage",
    "Bytes / sec",
)
save_chart(
    "overhead.png",
    time_steps,
    {
        "Deterministic": det["overhead"],
        "Probabilistic": prob["overhead"],
        "Adaptive": adapt["overhead"],
        "Prob-Adaptive": probadapt["overhead"],
    },
    "Cumulative Overhead",
    "Bytes (Log Scale)",
    log_scale=True,
)
save_chart(
    "memory.png",
    time_steps,
    {
        "Deterministic": det["mem"],
        "Probabilistic": prob["mem"],
        "Adaptive": adapt["mem"],
        "Prob-Adaptive": probadapt["mem"],
    },
    "Memory Usage",
    "Megabytes (MB)",
    is_line=True,
)
save_chart(
    "broadcast_latency.png",
    time_steps,
    {
        "Deterministic": det["bcast"],
        "Probabilistic": prob["bcast"],
        "Adaptive": adapt["bcast"],
        "Prob-Adaptive": probadapt["bcast"],
    },
    "Average Broadcast Latency",
    "Microseconds (µs)",
    is_line=True,
)
