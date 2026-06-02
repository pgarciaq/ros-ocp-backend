# Dual Engine (Cost vs Performance)

!!! info "Quick Facts"
    **Query param:** `?engine=cost` or `?engine=performance` (where supported)  
    **Default:** cost engine for savings aggregation and node list sorting  
    **Applies to:** container, namespace, node recommendations  
    **Configurable:** Percentiles and targets are tenant-tunable

## Overview

ROS-OCP produces **two recommendation perspectives** for the same workload data.
Both are computed on every ingestion cycle and stored side by side. The **cost
engine** minimizes resource allocation; the **performance engine** maximizes
reliability headroom.

Think of it as answering two questions:

- *"How small can we safely go?"* → **cost**
- *"How much headroom do we need for spikes and SLAs?"* → **performance**

## Cost engine

Optimizes for **lower resource cost** and higher cluster density:

| Resource | Cost engine behavior |
|----------|---------------------|
| CPU | Lower percentile (default P60) |
| Memory | P95 (still conservative vs CPU due to OOM risk) |
| Nodes | 80% target utilization; consolidates underutilized nodes aggressively |
| Savings | Higher estimated monthly savings (smaller requests) |

Best for: development, staging, batch workloads, cost-sensitive environments.

## Performance engine

Optimizes for **reliability and burst tolerance**:

| Resource | Performance engine behavior |
|----------|----------------------------|
| CPU | High percentile (default P98) |
| Memory | Max observed (P100) |
| Nodes | 55% target utilization; consolidates only with 2× headroom on both CPU and memory |
| Savings | Lower savings (or negative — additional cost for headroom) |

Best for: production, latency-sensitive services, SLA-critical workloads.

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

## Example: same container, two engines

A deployment with variable CPU spikes might receive:

| Engine | CPU request | Memory request |
|--------|-------------|----------------|
| cost | 2 cores | 512 MiB |
| performance | 4 cores | 768 MiB |

The performance engine covers more usage peaks; the cost engine rightsizes toward
typical load. **Savings estimates differ** — cost shows higher savings; performance
may show zero or negative savings when headroom increases cost.

## Configuration

Engine behavior is controlled by percentile and target parameters:

| Plugin | Cost knobs | Performance knobs |
|--------|------------|-------------------|
| Container / namespace | `cpu_cost_percentile`, `mem_cost_percentile` | `cpu_perf_percentile`, `mem_perf_percentile` |
| Node | `cost_target_utilization` | `perf_target_utilization`, `perf_consolidation_headroom_multiplier` |

Tune via [Configurable Thresholds](configurable-thresholds.md) or admin env vars.

## Future work

- **UI settings:** Expose cost vs performance percentile tuning in the UI (backend already supports this via `GET/PUT .../settings/container`).
- **UI history/quality:** Wire the engine selector in the frontend to history and quality endpoints (`filter[engine]`).

## Related

- [Container Right-Sizing](container-recommendations.md) — Engine output fields
- [Node Consolidation](node-recommendations.md) — Consolidation differences
- [Savings Estimations](savings-estimations.md) — Engine filter on fleet summary
- [Recommendation Engines](../architecture/recommendation-engines.md#summary-matrix)
