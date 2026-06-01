# Node Consolidation & Right-Sizing

!!! info "Quick Facts"
    **API:** `GET /api/cost-management/v1/recommendations/openshift/nodes`  
    **Configurable:** Yes  
    **Engines:** cost, performance (filter with `?engine=cost` or `?engine=performance`)  
    **Savings:** Yes — `estimated_monthly_savings` (`value` + `units`) per engine row

## Overview

Node recommendations analyze cluster-level CPU and memory utilization to identify
waste and sizing opportunities. Each node receives a **classification** (shared
across engines) plus **engine-specific sizing** and optional **consolidation**
guidance — recommending fewer nodes when workloads fit at a target utilization.

This is distinct from [GPU Time-Slicing](gpu-time-slicing.md), which covers
software GPU sharing at `GET .../gpu/timeslicing`.

## How it works

```mermaid
flowchart TD
  ND[Node daily digests] --> Class[classifyNode]
  Class --> Shared[Shared classification]
  Shared --> Cost[cost engine: 80% target]
  Shared --> Perf[performance engine: 55% target]
  Cost --> Out[recommended_cpu_cores, node_count_reduction]
  Perf --> Out
```

1. **Digest aggregation** — Node allocatable capacity, P95 usage, and pod
   request totals are computed per day within the term window.
2. **Classification** — Each node is labeled once (see table below).
3. **Dual-engine sizing** — Recommended capacity =
   `max(usage_p95, requests) / target_utilization`.
4. **Consolidation (Level 3)** — When underutilized, engines compute a per-node
   consolidation flag, then [`applyInstanceTypeConsolidation`](../../internal/engine/recommend_nodes.go)
   groups nodes by `instance_type` (from operator ROS CSV) and distributes
   `node_count_reduction` across the fleet. Nodes without `instance_type` keep
   legacy per-node binary reduction (0 or 1).
5. **Savings** — Dollar estimates compare current vs recommended node CPU, memory,
   and monthly node cost. See [Cost Integration — Node Savings](../architecture/cost-integration.md#node-savings-cpumemory-utilization).

## Classification types

| Type | Condition |
|------|-----------|
| **underutilized** | CPU P95 **and** memory P95 < 30% of allocatable |
| **overcommitted** | CPU requests / allocatable > 150% |
| **stranded_cpu** | EMA-smoothed CPU/memory imbalance > 0.6, CPU higher |
| **stranded_memory** | Same imbalance threshold, memory higher |
| **well_utilized** | None of the above |

Stranded-resource nodes have one dimension heavily used while the other sits idle
— a signal that instance type or workload placement may be mismatched.

## Dual engine behavior

| Aspect | Cost engine | Performance engine |
|--------|-------------|---------------------|
| Target utilization | 80% | 55% |
| Consolidation | Fleet-aware by `instance_type` when operator provides it | Same headroom guard; may assign `node_count_reduction = 0` when group cannot consolidate |
| Savings | More aggressive consolidation | Conservative; preserves headroom |

Filter list results: `?engine=cost` (default for sorting) or `?engine=performance`.

## Sizing output

| Field | Meaning |
|-------|---------|
| `recommended_cpu_cores` | Target CPU capacity for the node (or cluster slice) |
| `recommended_memory_gib` | Target memory capacity (GiB) |
| `node_count_reduction` | Suggested nodes to remove in this engine/term (0 or 1 per row; fleet sum may exceed 1) |
| `classification.idle_state` | `active`, `idle`, or `zombie` (node idle detection; migration **000111**) |
| `instance_type` | Cloud or cluster instance type from ROS metrics (e.g. `m5.xlarge`); omitted when unknown |
| `estimated_monthly_savings` | Dollar delta vs current allocation (`value` + `units`) |

## Term support

Short, medium, and long terms use the same defaults as container (1d / 7d / 15d).
List API defaults to **medium** term for classification display; all terms are
nested under `recommendation_terms`.

## API

```http
GET /api/cost-management/v1/recommendations/openshift/nodes
```

Query parameters include `cluster_uuid`, `node`, `engine`, `term`,
`filter[idle_state]` (`active`, `idle`, `zombie`; comma-separated),
`is_underutilized`, `is_overcommitted`, `order_by` (default
`estimated_monthly_savings`; alias `estimated_monthly_savings_usd`), and pagination.

### Example (abbreviated)

```json
{
  "meta": { "count": 12, "currency": "USD" },
  "data": [{
    "node": "worker-3.example.com",
    "cluster_uuid": "...",
    "recommendation_type": "underutilized",
    "recommendation_terms": {
      "medium_term": {
        "recommendation_engines": {
          "cost": {
            "recommended_cpu_cores": 8,
            "recommended_memory_gib": 32,
            "node_count_reduction": 1,
            "estimated_monthly_savings": {
              "value": "850.000000",
              "units": "USD"
            }
          },
          "performance": {
            "recommended_cpu_cores": 12,
            "recommended_memory_gib": 48,
            "node_count_reduction": 0,
            "estimated_monthly_savings": {
              "value": "200.000000",
              "units": "USD"
            }
          }
        }
      }
    }
  }]
}
```

## Configurable thresholds

`GET/PUT/DELETE .../settings/thresholds?recommendation_type=node`

| Parameter | Default | Purpose |
|-----------|---------|---------|
| `underutil_threshold` | 0.30 | P95 util below → underutilized |
| `overcommit_threshold` | 1.50 | Request/allocatable ratio alert |
| `allocatable_factor` | 0.93 | Fallback when allocatable unknown |
| `stranded_imbalance_threshold` | 0.60 | CPU/memory imbalance detection |
| `ema_alpha` | 0.30 | EMA smoothing for trends |
| `cost_target_utilization` | 0.80 | Cost engine sizing target |
| `perf_target_utilization` | 0.55 | Performance engine sizing target |
| `perf_consolidation_headroom_multiplier` | 2.0 | Performance consolidation guard |
| `trend_min_days` | 3 | Min days for CPU trend slope |

Env vars: `ROS_NODE_*` — see [Configurability](../architecture/configurability.md#node).

## Roadmap / deferred

### Intentionally out of scope (Tier 1)

| Item | Rationale |
|------|-----------|
| **Business hours for nodes** | Nodes are always-on infrastructure; `idle_state` (`active` / `idle` / `zombie`) covers decommissioning without schedule complexity. Container and namespace recommendations retain business-hours support. |

### Planned future work (Tier 2 & Tier 3)

Tier 1 (this document) is **implemented**. The next tiers target the **actionable unit** in managed OpenShift: the **MachineSet**, then the **MachineAutoscaler**.

Full design, prerequisites, effort estimates, and schema notes:
**[Node recommendations roadmap — Tier 2 & Tier 3](../architecture/node-recommendations-roadmap.md)**.

#### Tier 2 — MachineSet right-sizing (~2–3 weeks)

**Goal:** Group nodes by MachineSet and recommend replica count and instance family/size at the MachineSet level.

| Deliverable | Summary |
|-------------|---------|
| Operator | Emit `machineset_name` from `machine.openshift.io/machine-set` (and replica counts for Tier 3) on ROS node CSV |
| Ingestion | Populate existing `machineset_name` on `daily_node_digests` |
| Engine | New `machineset` plugin: aggregate per-MachineSet, optimal replicas + instance type via **cloud instance catalog** |
| API | `GET /recommendations/openshift/machinesets` + `machineset_recommendations` table |
| Catalog | AWS/Azure/GCP instance specs (and optional pricing) for family/size recommendations |

**Limitation:** Only clusters using MachineSets (IPI). Bare metal, SNO, and UPI without Machine API stay on Tier 1 only (`machineset_name` NULL).

#### Tier 3 — MachineAutoscaler optimization (~4–6 weeks after Tier 2)

**Goal:** Analyze historical replica counts vs demand; recommend tighter `minReplicas`/`maxReplicas`; flag saturated, idle, or flapping autoscalers; optional scaling-policy guidance.

| Prerequisite | Why |
|--------------|-----|
| Tier 2 complete | MachineSet identity and replica metadata |
| Operator | MachineAutoscaler min/max/current + scaling history or hourly snapshots |
| Engine | Time-series analysis (not just point-in-time utilization) |

**Complexity:** More research-oriented than Tiers 1–2 (scheduling bursts, PDB/drain safety). Notification codes **14**, **16–17** are reserved — see [notification codes](../architecture/notification-codes.md).

## Related

- [Node recommendations roadmap (Tier 2 & 3)](../architecture/node-recommendations-roadmap.md) — MachineSet and MachineAutoscaler planned work
- [Dual Engine](dual-engine.md) — Cost vs performance trade-offs
- [Savings Estimations](savings-estimations.md) — Fleet-level node savings totals
- [GPU Time-Slicing](gpu-time-slicing.md) — Separate node-level GPU feature
