# Recommendation Math

This document describes the mathematical algorithms used in the ROS-OCP-Backend native recommendation engine.

## CPU Recommendation

### Algorithm

1. **Weighted Percentile**: Compute decay-weighted percentile of daily CPU usage
2. **Adaptive Margin**: Apply a dynamic safety margin based on workload variability
3. **Floor**: Enforce minimum resource allocation
4. **Limit**: Set limit as a multiplier of request

```
cost_request = max(floor, round(WeightedPercentile(usage, cost_pctile) × adaptive_margin))
perf_request = max(floor, round(WeightedPercentile(usage, perf_pctile) × adaptive_margin))
cost_limit = round(cost_request × limit_multiplier)
perf_limit = round(perf_request × limit_multiplier)
```

### Default Parameters

| Parameter | Cost Profile | Performance Profile |
|-----------|-------------|-------------------|
| Percentile | P60 | P98 |
| Min margin | 1.15 (15%) | 1.15 (15%) |
| Max margin | 1.50 (50%) | 1.50 (50%) |
| Limit multiplier | 2.0 | 2.0 |
| Floor | 10 mc (millicores) | 10 mc |

## Memory Recommendation

Same structure as CPU with memory-specific percentiles and OOM feedback:

1. If OOM events detected: bump recommendation by `ROS_OOM_BASE_BUMP` (default 20%) with exponential backoff up to `ROS_OOM_MAX_BUMP` (default 100%)
2. Memory uses MiB (mebibytes) as the unit

## Decay Weighting

Decay weighting gives more importance to recent data:

```
weight(row) = exp(-ln(2) × age_hours / half_life_hours)
```

Where `age_hours = (now - row.interval_start).Hours()`

| Term | Window | Half-Life | Effect |
|------|--------|-----------|--------|
| Short | 1 day | 0 (no decay) | Equal weight to all data in window |
| Medium | 7 days | 168h (7 days) | 50% weight at 7 days old |
| Long | 15 days | 360h (15 days) | 50% weight at 15 days old |

When `half_life_hours = 0`, all data points receive equal weight (no decay). This is intentional for the short term where the window is already 1 day.

### Decay Design Notes

- Decay is **hour-based**, not calendar-day-based. This avoids DST complications (all timestamps are UTC).
- The ~1h skew from hour-based vs day-based decay is negligible on 7–15 day windows.
- Upper bound validation: half-life is capped at 8760 hours (1 year) in the API.

## Weighted Percentile

The `WeightedPercentile` function computes a decay-weighted percentile:

1. Extract the target metric from each digest row via a selector function
2. Compute decay weight for each row based on age
3. Sort rows by metric value
4. Find the value at the weighted cumulative distribution position matching the target percentile

This is NOT an interpolated percentile — it selects the closest actual observation.

## Adaptive Margin

The margin adapts to workload variability:

```
variability = (p95 - p50) / mean
margin = clamp(1.0 + variability × 0.5, min_margin, max_margin)
```

- Stable workloads (low variability): margin approaches `min_margin` (15%)
- Bursty workloads (high variability): margin approaches `max_margin` (50%)
- Zero mean: falls back to `max_margin`

## Trend Detection

Linear regression slope on P98 CPU/memory values across days:

```
slope = Σ((x - x̄)(y - ȳ)) / Σ((x - x̄)²)
```

Where x = day index, y = P98 value. A positive slope indicates growing resource demand.

**Minimum data**: Trend is meaningful only with ≥3 data points. With 1–2 points, the slope is set to 0.

## Idle Detection

A container is classified as **idle** when:

- CPU P95 < `DefaultIdleThresholdMC` (10 millicores) AND
- Memory P95 < `DefaultIdleThresholdMemKiB` (10 MiB = 10240 KiB)

Idle containers receive 100% savings estimation (recommend deallocation).

## Abandoned Detection

A container is **abandoned** when ALL usage metrics are exactly zero across all digests in the window. This is stricter than idle — zero usage means the container exists but does absolutely nothing.

## Namespace Recommendations

Namespace recommendations aggregate container recommendations within a namespace:

- CPU/memory recommendations summed across containers
- P60/P98/P99 percentiles computed from namespace-level digests
- Trend slope computed from namespace aggregate usage

## Node Recommendations

Node-level recommendations classify nodes by utilization patterns:

| Classification | Condition |
|---------------|-----------|
| Underutilized | CPU P95 < `ROS_NODE_UNDERUTIL_THRESHOLD` (default 0.30) |
| Overcommitted | Total requests > allocatable × `ROS_NODE_OVERCOMMIT_THRESHOLD` (default 0.90) |
| Stranded | EMA-smoothed `|cpu_p95 - mem_p95| / max(cpu_p95, mem_p95)` > `ROS_NODE_STRANDED_IMBALANCE_THRESHOLD` (default 0.60) |
| Healthy | None of the above |

Node EMA smoothing uses `ROS_NODE_EMA_ALPHA` (default 0.3) to filter noise from daily utilization before trend/classification.

## Source Files

- CPU: [`internal/engine/recommend_cpu.go`](../../internal/engine/recommend_cpu.go)
- Memory: [`internal/engine/recommend_memory.go`](../../internal/engine/recommend_memory.go)
- Decay/percentile: [`internal/engine/percentile.go`](../../internal/engine/percentile.go)
- Margin: [`internal/engine/margin.go`](../../internal/engine/margin.go)
- Trend: [`internal/engine/trend.go`](../../internal/engine/trend.go)
- Idle: [`internal/engine/detect_idle.go`](../../internal/engine/detect_idle.go)
- Term config: [`internal/engine/term_config.go`](../../internal/engine/term_config.go)
- Node: [`internal/engine/recommend_nodes.go`](../../internal/engine/recommend_nodes.go)
