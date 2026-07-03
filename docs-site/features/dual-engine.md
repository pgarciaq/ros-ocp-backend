# Dual Engine (Cost vs Performance)

!!! info "Quick Facts"
    **Query param:** `?engine=cost` or `?engine=performance` (where supported)  
    **Default:** cost engine for savings aggregation and node list sorting  
    **Applies to:** container, namespace, node, VM recommendations  
    **Configurable:** Percentiles and targets are tenant-tunable

## Overview

ROS-OCP produces **two recommendation perspectives** for the same workload data.
Both are computed on every ingestion cycle and stored side by side. The **cost
engine** minimizes resource allocation; the **performance engine** maximizes
reliability headroom.

Think of it as answering two questions:

- *"How small can we safely go?"* → **cost**
- *"How much headroom do we need for spikes and SLAs?"* → **performance**

Default container/namespace percentiles (cost **P60** CPU / **P95** memory; performance **P98** CPU / **P100** memory) and node target utilization (cost **80%**, performance **55%**) are documented in the [Recommendation Engines reference](../architecture/recommendation-engines.md#engine-percentiles-and-sizing) and [node engine parameters](../architecture/recommendation-engines.md#engine-specific-parameters).

## Background: What are percentiles?

To understand the two engines, you first need to understand the statistics they
use. The operator samples container CPU and memory usage every hour. At the end
of each day, these samples are summarized into a **daily digest** containing
several statistics:

**Mean** — the arithmetic average of all hourly samples in the day. Gives you
the "typical" level, but a single spike pulls it up, and it tells you nothing
about how spread out the values are.

**P50 (median)** — the value that 50% of samples fall below. Unlike the mean,
extreme spikes barely move it. If a container's P50 memory is 200 MiB, it spent
half the day below 200 MiB and half above.

**P95** — the value that 95% of samples fall below. It captures "near-worst-case"
usage while ignoring the most extreme outliers. Only about 1 in 20 samples
exceeded this level.

**P98** — the value that 98% of samples fall below. Even more conservative than
P95 — only about 1 in 50 samples exceeded this.

**Max (P100)** — the single highest value observed. It captures the absolute
worst case, including rare spikes. Nothing was ever higher during the
observation window.

### Intuition

- **P50** and **Mean** describe what the container *usually* does
- **P95** describes what the container does under *heavy* load (excluding rare extremes)
- **P98** is close to worst-case but still filters out the single most extreme spike
- **Max** describes the *worst* the container has ever done

A recommendation based on P60 says "cover 60% of observed usage" — cheap, but
40% of the time actual usage exceeds the recommendation. A recommendation based
on Max says "cover 100% of observed usage" — expensive, but actual usage should
never exceed it.

## How the engines use percentiles

Both engines follow the same four-step formula. They differ only in **which
percentile column** they read from the daily digests:

```
request = max(floor, WeightedPercentile(usage, percentile) × adaptive_margin)
limit   = request × limit_multiplier
```

1. **Pick the percentile** — the cost engine picks a lower percentile; the
   performance engine picks a higher one.
2. **Decay-weighted average** — recent days count more than older days (see
   [Decay Weights](../architecture/decay-weights.md)).
3. **Adaptive margin** — a safety buffer (1.15×–1.50×) that scales with
   workload variability. Stable workloads get tighter margins; bursty ones get
   more headroom. This margin is the **same** for both engines.
4. **Limit** — set as `request × 1.05` (5% above request). This is also the
   **same** for both engines.

The only difference between the two engines is step 1: which percentile they
start from. Everything else — margin, limit multiplier, OOM bump, floor — is
identical.

### Default percentiles

| Resource | Cost engine | Performance engine |
|----------|------------|-------------------|
| CPU      | P60        | P98               |
| Memory   | P95        | Max (P100)        |

Memory uses higher percentiles than CPU in both engines because the consequence
of exceeding a memory limit is an **OOM kill** (container is terminated), while
exceeding a CPU limit only causes **throttling** (container slows down but
survives).

## Worked example: same container, two engines

Consider a container whose daily memory usage digests over the past week are:

| Day | P50 | P95 | Max | Mean |
|-----|-----|-----|-----|------|
| Mon | 200 MiB | 350 MiB | 500 MiB | 220 MiB |
| Tue | 210 MiB | 340 MiB | 480 MiB | 215 MiB |
| Wed | 190 MiB | 360 MiB | 520 MiB | 210 MiB |
| Thu | 205 MiB | 345 MiB | 490 MiB | 218 MiB |
| Fri | 195 MiB | 355 MiB | 510 MiB | 212 MiB |
| Sat | 180 MiB | 330 MiB | 460 MiB | 200 MiB |
| Sun | 185 MiB | 335 MiB | 470 MiB | 205 MiB |

**Step 1: Pick the percentile column.**

- Cost engine reads the **P95 column**: `[350, 340, 360, 345, 355, 330, 335]`
- Performance engine reads the **Max column**: `[500, 480, 520, 490, 510, 460, 470]`

**Step 2: Decay-weighted average.**

After applying recency weighting (recent days count more):

- Cost base: **~348 MiB**
- Performance base: **~505 MiB**

**Step 3: Adaptive margin.**

For this moderately variable workload, say the adaptive margin works out to
**1.25** (25% buffer). This is the same for both engines because it's computed
from the same underlying variability metrics.

- Cost request: 348 × 1.25 = **435 MiB**
- Performance request: 505 × 1.25 = **631 MiB**

**Step 4: Limit (request × 1.05).**

- Cost limit: 435 × 1.05 = **457 MiB**
- Performance limit: 631 × 1.05 = **663 MiB**

### Why cost means more OOM risk

In Kubernetes, an **OOM kill** happens when a container's actual memory usage
exceeds its **limit**. So the recommended limit determines the OOM threshold:

- The **cost** engine recommends a limit of **457 MiB**. But the container's
  observed peaks were 460–520 MiB. Actual spikes **exceed** this limit. OOM
  kills will happen periodically.
- The **performance** engine recommends a limit of **663 MiB**. The container's
  observed peaks were 460–520 MiB, well below this limit. OOM kills are
  unlikely.

The gap between the recommended limit and actual peak usage is the
**headroom** — the breathing room for unexpected spikes:

- Cost headroom: 457 − 520 = **−63 MiB** (negative — the limit is *below* peak
  usage, so OOM kills are expected)
- Performance headroom: 663 − 520 = **+143 MiB** (positive — comfortable buffer
  for spikes)

## Cost engine

Optimizes for **lower resource cost** and higher cluster density:

| Resource | Cost engine behavior |
|----------|---------------------|
| CPU | Lower percentile (default P60) — sizes for typical load, accepts throttling during spikes |
| Memory | P95 — sizes for heavy load, but rare peaks may cause OOM kills |
| Nodes | 80% target utilization; consolidates underutilized nodes aggressively |
| Savings | Higher estimated monthly savings (smaller requests) |

Best for: development, staging, batch workloads, cost-sensitive environments
where occasional OOM kills or CPU throttling are acceptable.

## Performance engine

Optimizes for **reliability and burst tolerance**:

| Resource | Performance engine behavior |
|----------|----------------------------|
| CPU | High percentile (default P98) — sizes for near-worst-case, throttling is rare |
| Memory | Max observed (P100) — sizes for the worst spike ever seen, OOM kills are unlikely |
| Nodes | 55% target utilization; consolidates only with 2× headroom on both CPU and memory |
| Savings | Lower savings (or negative — additional cost for headroom) |

Best for: production, latency-sensitive services, SLA-critical workloads where
OOM kills or CPU throttling would violate SLAs.

## Summary

| | Cost engine | Performance engine |
|---|---|---|
| **Goal** | Save money | Avoid resource starvation |
| **Sizes for** | What the container *usually* needs | What the container has *ever* needed |
| **CPU percentile** | P60 | P98 |
| **Memory percentile** | P95 | Max (P100) |
| **Recommended request** | Smaller | Larger |
| **Recommended limit** | Smaller | Larger |
| **Headroom** | Less (or negative) | More |
| **OOM risk** | Higher | Lower |
| **Cost** | Lower | Higher |
| **Best for** | Dev, staging, batch | Production, SLA-critical |

## Where it applies

| Plugin | API behavior |
|--------|--------------|
| **Container** | Both engines nested under every term; `filter[engine]=cost\|performance` on list (omits the other engine from `recommendation_engines`) |
| **Namespace** | Same as container — `filter[engine]` on namespace list |
| **Node** | Both engines nested; `filter[engine]=cost\|performance` on `/nodes` list |
| **VM** | Both engines stored per VM × term; `filter[engine]=cost\|performance` on list/detail — **native only** (Kruize does not support VMs) |
| **History** | `filter[engine]=cost\|performance` on `/history` and namespace history |
| **Quality** | `filter[engine]=cost\|performance` on `/quality` (defaults to `cost` when omitted) |
| **GPU, PVC, Snapshot** | Single engine only (no cost/performance split) |

Business hours adds a second **schedule** dimension (all_hours vs business_hours)
on top of engines for container and namespace. See [Business Hours](business-hours.md).

## How to select an engine

| Context | Selection |
|---------|-----------|
| Container/namespace list API | `filter[engine]=cost` or `filter[engine]=performance` (legacy flat `?engine=` also accepted) |
| Container/namespace UI | Display one engine tab; use `filter[engine]` when loading a single perspective |
| Node list | `filter[engine]=cost` or `filter[engine]=performance` |
| History / quality | `filter[engine]=cost` or `filter[engine]=performance` (quality defaults to cost) |
| Fleet savings | `GET .../savings-summary?engine=cost` (default) |
| CSV export | One row per term × engine |

## Configuration

Engine behavior is controlled by percentile and target parameters:

| Plugin | Cost knobs | Performance knobs |
|--------|------------|-------------------|
| Container / namespace | `cpu_cost_percentile`, `mem_cost_percentile` | `cpu_perf_percentile`, `mem_perf_percentile` |
| Node | `cost_target_utilization` | `perf_target_utilization`, `perf_consolidation_headroom_multiplier` |

Tune via [Configurable Thresholds](configurable-thresholds.md) or admin env vars.

## Verifying divergence

Cost and performance engines are always computed together, but sizing may match on
uniform workloads. To **force** different CPU/memory recommendations:

1. Ingest cluster data from the NISE fixture [`nise/examples/ocp_dual_engine/`](../../../nise/examples/ocp_dual_engine/README.md) (`spike-cpu-api`, `steady-mem-worker`).
2. Call a list or detail endpoint without `filter[engine]` and compare
   `recommendation_terms.<term>.recommendation_engines.cost` vs `.performance`.
3. Expect higher CPU/memory on the **performance** engine for spike-prone containers;
   node list may show different `recommended_cpu_cores` / `node_count_reduction` per engine.

E2E and IQE tests assert both engines are present; when values are equal they emit a
warning and point to this fixture rather than skipping.

## Future work

- **UI settings:** Expose cost vs performance percentile tuning in the UI (backend already supports this via `GET/PUT .../settings/container`).
- **UI history/quality:** Wire the engine selector in the frontend to history and quality endpoints (`filter[engine]`).

## Related

- [Container Right-Sizing](container-recommendations.md) — Engine output fields
- [Node Consolidation](node-recommendations.md) — Consolidation differences
- [Savings Estimations](savings-estimations.md) — Engine filter on fleet summary
- [Recommendation Engines](../architecture/recommendation-engines.md#summary-matrix)
