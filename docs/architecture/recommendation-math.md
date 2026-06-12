# Recommendation Math

This document describes the mathematical algorithms used in the ROS-OCP-Backend native recommendation engine.

> **Complete parameter reference:** For all default thresholds, percentiles, term windows,
> and environment variables across every plugin, see
> [Recommendation Engine Reference](recommendation-engines.md).

## CPU Recommendation

### Algorithm

1. **Weighted Percentile**: Compute decay-weighted average of daily CPU usage at the target percentile
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

| Parameter | Cost Profile | Performance Profile | Env override |
|-----------|-------------|---------------------|--------------|
| Percentile | P60 | P98 | — (compiled defaults in [`types.go`](../../internal/engine/types.go)) |
| Min margin | 1.15 (15%) | 1.15 (15%) | — |
| Max margin | 1.50 (50%) | 1.50 (50%) | — |
| Limit multiplier | 1.05 | 1.05 | — |
| Floor | 25 mc (millicores) | 25 mc | — |

## Memory Recommendation

Same structure as CPU with memory-specific percentiles and OOM feedback:

1. Compute decay-weighted average at the profile percentile (cost: P95, performance: max/P100)
2. Apply the same adaptive margin as CPU
3. If OOM events detected: multiply request by `min(ROS_OOM_MAX_BUMP, 1.0 + ROS_OOM_BASE_BUMP × log₂(1 + OOMCount))`
   - Defaults: `ROS_OOM_BASE_BUMP` = **0.15**, `ROS_OOM_MAX_BUMP` = **1.60** (cap at 60% bump)
4. Set limit = `round(request × 1.05)` (same limit multiplier as CPU)
5. Memory uses MiB (mebibytes) as the unit; there is no memory floor constant

| Parameter | Cost Profile | Performance Profile | Env override |
|-----------|-------------|---------------------|--------------|
| Percentile | P95 | Max (P100) | — |
| Min / max margin | 1.15 / 1.50 | 1.15 / 1.50 | — |
| Limit multiplier | 1.05 | 1.05 | — |
| OOM bump | `min(1.60, 1.0 + 0.15 × log₂(1 + OOMCount))` | same | `ROS_OOM_BASE_BUMP`, `ROS_OOM_MAX_BUMP` |

## Decay Weighting

For visual charts, edge-weight tables, configuration examples, and performance
notes on lookup tables, see [Decay Weights](decay-weights.md).

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

The `WeightedPercentile` function computes a decay-weighted average of values extracted from daily digest rows:

1. Extract the target metric from each digest row via a selector function (pre-computed daily percentile column)
2. Compute decay weight for each row based on age
3. Return the weighted average: `round(Σ(value × weight) / Σ(weight))`

This is NOT an interpolated percentile — it averages pre-computed daily percentile observations with recency weighting.

## Adaptive Margin

The margin adapts to workload variability using decay-weighted averages of daily P95, P50, and mean:

```
variability = (p95 - p50) / mean
margin = clamp(1.0 + variability, min_margin, max_margin)
```

- Stable workloads (low variability): margin approaches `min_margin` (15%)
- Bursty workloads (high variability): margin approaches `max_margin` (50%)
- Zero or negative mean: falls back to `min_margin` (1.15)

## Trend Detection

Linear regression slope on daily digest values across the term window:

```
slope = Σ((x - x̄)(y - ȳ)) / Σ((x - x̄)²)
```

Where x = day index (0-based), y = selected metric. A positive slope indicates growing resource demand.

| Scope | Metric used | Minimum data |
|-------|-------------|--------------|
| Container CPU | P98 (`CPUUsageP98MC`) | ≥ 2 days (otherwise slope = 0) |
| Container memory | P95 (`MemUsageP95KiB`) | ≥ 2 days (otherwise slope = 0) |
| Node CPU utilization | EMA-smoothed daily P50 utilization | ≥ 3 valid days |

## Idle Detection

A container is classified as **idle** when **every** digest row in the term window satisfies:

- `CPUUsageMaxMC` < `DefaultIdleThresholdMC` (**10** millicores) **and**
- `MemUsageMaxKiB` < `DefaultIdleThresholdMemKiB` (**10240** KiB = 10 MiB)

Idle containers receive 100% savings estimation (recommend deallocation).

Constants are defined in [`detect_idle.go`](../../internal/engine/detect_idle.go) (not env-configurable).

## Abandoned Detection

A container is **abandoned** when ALL usage metrics are exactly zero across all digests in the window. This is stricter than idle — zero usage means the container exists but does absolutely nothing.

## Namespace Recommendations

Namespace recommendations aggregate container recommendations within a namespace:

- CPU/memory recommendations summed across containers
- P60/P98/P99 percentiles computed from namespace-level digests
- Trend slope computed from namespace aggregate usage
- Uses the same sizing parameters as container (see [Recommendation Engine Reference](recommendation-engines.md#container-namespace-recommendations))

## Node Recommendations

Node-level recommendations classify nodes by utilization patterns:

| Classification | Condition |
|---------------|-----------|
| Underutilized | Avg CPU P95 **and** avg mem P95 < `ROS_NODE_UNDERUTIL_THRESHOLD` (default **0.30**) |
| Overcommitted | Max CPU requests / allocatable > `ROS_NODE_OVERCOMMIT_THRESHOLD` (default **1.50**) |
| Stranded | EMA-smoothed `\|cpu_p95 - mem_p95\| / max(cpu_p95, mem_p95)` > `ROS_NODE_STRANDED_IMBALANCE_THRESHOLD` (default **0.60**); requires ≥ 2 valid days |
| Healthy | None of the above |

**Supporting parameters:**

| Parameter | Default | Env variable |
|-----------|---------|--------------|
| Allocatable fallback factor | 0.93 | `ROS_NODE_ALLOCATABLE_FACTOR` |
| EMA smoothing alpha | 0.3 | `ROS_NODE_EMA_ALPHA` |
| Cost engine target utilization | 80% | — (compiled in [`recommend_nodes.go`](../../internal/engine/recommend_nodes.go)) |
| Performance engine target utilization | 55% | — |

Node EMA smoothing uses `ROS_NODE_EMA_ALPHA` (default 0.3) to filter noise from daily utilization before trend/classification.

**Sizing:** recommended capacity = `max(usage_p95, requests) / target_utilization`.

## Source Files

- CPU: [`internal/engine/recommend_cpu.go`](../../internal/engine/recommend_cpu.go)
- Memory: [`internal/engine/recommend_memory.go`](../../internal/engine/recommend_memory.go)
- Decay/percentile: [`internal/engine/decay.go`](../../internal/engine/decay.go), [`internal/engine/percentile.go`](../../internal/engine/percentile.go)
- Margin: [`internal/engine/margin.go`](../../internal/engine/margin.go)
- Trend: [`internal/engine/trend.go`](../../internal/engine/trend.go)
- Idle: [`internal/engine/detect_idle.go`](../../internal/engine/detect_idle.go)
- Term config: [`internal/engine/term_config.go`](../../internal/engine/term_config.go)
- Defaults / OOM config: [`internal/engine/types.go`](../../internal/engine/types.go), [`internal/config/config.go`](../../internal/config/config.go)
- Node: [`internal/engine/recommend_nodes.go`](../../internal/engine/recommend_nodes.go)
