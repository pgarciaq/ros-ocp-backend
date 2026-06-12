#!/usr/bin/env python3
"""Generate decay weight distribution charts for documentation."""

from __future__ import annotations

import math
from pathlib import Path

import matplotlib.pyplot as plt
import numpy as np

REPO_ROOT = Path(__file__).resolve().parent.parent
OUTPUT_DIRS = [
    REPO_ROOT / "docs-site" / "architecture" / "charts",
    REPO_ROOT / "docs" / "performance" / "charts",
]

TERM_DAYS = 30
HALF_LIVES = [10, 15, 20]

# Visual palette
WINDOW_FILL = "#FFF9C4"  # pale yellow
CURVE_FILL = "#BBDEFB"  # pale blue
CURVE_LINE = "#1976D2"  # darker blue
HALF_LINE = "#757575"  # dashed 50% reference
EDGE_ANNOTATION = "#424242"


def decay_weight(days_ago: float, half_life_days: float) -> float:
    """Exponential decay: 2^(-daysAgo / halfLifeDays)."""
    if half_life_days <= 0:
        return 1.0
    return math.exp(-days_ago * math.log(2) / half_life_days)


def edge_weight_pct(half_life_days: float) -> str:
    weight = decay_weight(TERM_DAYS, half_life_days)
    return f"{weight * 100:.1f}%"


def render_chart(half_life_days: int, output_path: Path) -> None:
    days = np.linspace(0, TERM_DAYS, 500)
    weights = np.array([decay_weight(d, half_life_days) for d in days])

    plt.style.use("seaborn-v0_8-whitegrid")
    fig, ax = plt.subplots(figsize=(8, 4.5), dpi=120)

    # Term window background (pale yellow)
    ax.axhspan(0, 1.0, xmin=0, xmax=1, facecolor=WINDOW_FILL, zorder=0)
    ax.axvspan(0, TERM_DAYS, facecolor=WINDOW_FILL, zorder=0)

    # Filled area under curve
    ax.fill_between(days, weights, color=CURVE_FILL, alpha=0.9, zorder=1)
    ax.plot(days, weights, color=CURVE_LINE, linewidth=2.2, zorder=2)

    # 50% reference line and half-life intersection
    ax.axhline(0.5, color=HALF_LINE, linestyle="--", linewidth=1.2, zorder=3)
    ax.plot(
        half_life_days,
        0.5,
        marker="o",
        markersize=7,
        color=CURVE_LINE,
        zorder=4,
    )
    ax.annotate(
        f"Half-life: {half_life_days}d",
        xy=(half_life_days, 0.5),
        xytext=(half_life_days + 2.5, 0.62),
        fontsize=9,
        color=EDGE_ANNOTATION,
        arrowprops=dict(arrowstyle="->", color=HALF_LINE, lw=0.9),
    )
    ax.text(
        half_life_days,
        0.5,
        "  50%",
        fontsize=8,
        color=HALF_LINE,
        va="center",
        ha="left",
    )

    # Edge weight at window boundary
    edge_w = decay_weight(TERM_DAYS, half_life_days)
    ax.plot(TERM_DAYS, edge_w, marker="o", markersize=6, color=CURVE_LINE, zorder=4)
    ax.annotate(
        f"Edge weight: {edge_weight_pct(half_life_days)}",
        xy=(TERM_DAYS, edge_w),
        xytext=(TERM_DAYS - 14, edge_w + 0.12),
        fontsize=9,
        color=EDGE_ANNOTATION,
        arrowprops=dict(arrowstyle="->", color=HALF_LINE, lw=0.9),
    )

    ax.set_xlim(0, TERM_DAYS)
    ax.set_ylim(0, 1.05)
    ax.set_xlabel("Days ago", fontsize=11)
    ax.set_ylabel("Weight", fontsize=11)
    ax.set_title(
        f"Decay Weights: {TERM_DAYS}-day window, {half_life_days}-day half-life",
        fontsize=12,
        fontweight="semibold",
        pad=12,
    )
    ax.set_xticks(np.arange(0, TERM_DAYS + 1, 5))
    ax.set_yticks(np.arange(0, 1.1, 0.1))
    ax.grid(True, alpha=0.35, zorder=0)
    ax.set_facecolor("white")
    fig.patch.set_facecolor("white")

    output_path.parent.mkdir(parents=True, exist_ok=True)
    fig.savefig(output_path, bbox_inches="tight", facecolor="white")
    plt.close(fig)


def main() -> None:
    for output_dir in OUTPUT_DIRS:
        output_dir.mkdir(parents=True, exist_ok=True)

    for half_life in HALF_LIVES:
        filename = f"decay-weights-30d-hl{half_life}d.png"
        for output_dir in OUTPUT_DIRS:
            render_chart(half_life, output_dir / filename)
        print(f"Wrote {filename} to {len(OUTPUT_DIRS)} directories")


if __name__ == "__main__":
    main()
