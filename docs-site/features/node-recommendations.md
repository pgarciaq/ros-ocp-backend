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
   groups nodes by `instance_type` (from operator ROS CSV) when present, or by
   **capacity-based fleet keys** when it is absent: allocatable CPU and memory rounded
   to one decimal core/GiB (~10% tolerance) so similarly sized nodes consolidate
   together. Fleet math distributes `node_count_reduction` across the group (not
   only legacy per-node 0/1 binary reduction). When `machineset_name` is present on
   digests, list responses include it for UI grouping; `GET .../machinesets` aggregates
   fleet savings by MachineSet (catalog-driven replica/instance-family recs remain
   planned — see roadmap).
5. **Instance type hints** — For stranded CPU/memory nodes, the engine may set
   `suggested_instance_type` and `instance_type_reason` by comparing capacity ratios
   across instance types already observed in the cluster (simplified catalog-free
   suggestion). Notification **13** also exposes `suggested_direction`
   (`memory-optimized` / `compute-optimized`).
6. **Savings** — Dollar estimates compare current vs recommended node CPU, memory,
   and monthly node cost. See [Cost Integration — Node Savings](../architecture/cost-integration.md#node-savings-cpumemory-utilization).
   When a Koku OCP cost model is updated, Koku notifies ROS to
   [recalculate persisted savings](../architecture/cost-integration.md#savings-recalculation-after-cost-model-changes)
   without waiting for the next operator upload.

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
| `machineset_name` | MachineSet label when reported by the operator |
| `suggested_instance_type` | In-cluster alternative instance type for stranded nodes (see above) |
| `instance_type_reason` | Explanation when `suggested_instance_type` is set |
| `pod_capacity` | Max schedulable pods (from ROS CSV); omitted when unknown |
| `pod_scheduling_headroom` | `(pod_capacity − pod_count) / pod_capacity` when capacity is known |
| `estimated_monthly_savings` | Dollar delta vs current allocation (`value` + `units`) |

## Term support

Short, medium, and long terms use the same defaults as container (1d / 7d / 15d).
List API defaults to **medium** term for classification display; all terms are
nested under `recommendation_terms`.

## Cold start behavior

Node recommendations require **`min_data_days`** of collected data per term before
the engine emits results for that term. This is intentional — not a bug.

| Term | Default `min_data_days` |
|------|-------------------------|
| short | 1 |
| medium | 3 |
| long | 7 |

The multi-term design handles cold start gracefully:

- **Short-term** recommendations can appear after **1 day** of node digests.
- **Medium-term** recommendations appear after **3 days**.
- **Long-term** recommendations appear after **7 days**.

Users see progressively more confident recommendations as data accumulates. Until
a term’s threshold is met, that term is omitted or empty in API responses
(`recommendation_terms`); other terms may still return data.

Override defaults per org:

```http
PUT /api/cost-management/v1/recommendations/openshift/settings/terms?recommendation_type=node
```

Body includes `terms[].min_data_days` (must be ≤ `window_days` for each term).
See [Configurability — Node terms](../architecture/configurability.md#node).

## Fleet consolidation — advisory only (Tier 1)

Tier 1 fleet consolidation is **intended for human review**, not unattended or automated execution. ROS does not scale MachineSets, drain nodes, or simulate the scheduler. It surfaces a FinOps signal: “this fleet likely has excess capacity.”

### What Tier 1 does

- Computes **`node_count_reduction`** per node (and fleet sums when nodes share a MachineSet, `instance_type`, or capacity bucket)
- Bases consolidation on **aggregate P95 CPU/memory utilization** vs engine target (80% cost / 55% performance)
- Groups nodes by **MachineSet** when `machineset_name` is on digests, else **`instance_type`**, else **allocatable capacity bucket** (~10% tolerance)
- Exposes **`GET .../machinesets`** to roll up savings by MachineSet for platform review

### Safety gate: pod scheduling headroom

Before assigning consolidation on a node, the cost/performance engines check **`pod_scheduling_headroom`**:

```
pod_scheduling_headroom = (pod_capacity − pod_count) / pod_capacity
```

When capacity is known and headroom falls below **`pod_headroom_consolidation_gate`** (default **0.15**, configurable via node thresholds), **`node_count_reduction` is forced to 0** even if the node is underutilized. That blocks recommendations that would imply absorbing more pod churn onto an already full node.

Notification **74** fires when headroom is below **`pod_headroom_notification_threshold`** (default **0.10**). The consolidation gate must be ≥ the notification threshold.

### What Tier 1 does not do

- **PodDisruptionBudgets** — no min-available or max-unavailable checks
- **Scheduling feasibility** — no simulation of taints, tolerations, affinity, DaemonSet placement, or zone spread
- **MachineSet replica counts** — operator does not yet emit OpenShift MachineSet desired/available replicas (planned for Tier 2/3)

This scope matches **industry-standard advisory consolidation**: AWS Compute Optimizer, Kubecost, and CAST AI (among others) recommend rightsizing and fleet reduction without PDB or scheduling simulation. Customers apply changes manually after operational review.

### Recommended action path (platform team)

1. Review list or **`GET .../machinesets`** — note `node_count_reduction`, `machineset_name`, and `estimated_monthly_savings`
2. Validate **PDBs**, maintenance windows, and workload SLOs for workloads on candidate nodes
3. Confirm **scheduling constraints** (taints, affinity, storage, burst capacity) on remaining nodes
4. **Scale down** the MachineSet or remove nodes through your normal change process (drain → delete → adjust replica count)

Treat savings figures as **estimates** for prioritization, not guaranteed post-change spend.

### Future: safe for automation (Tier 2+)

**Tier 2 (planned)** adds PDB snapshots, tolerations/affinity, and placement simulation so consolidation recommendations can be marked **safe to auto-execute** (with strong guardrails). **Tier 3 (planned)** ties recommendations to **MachineAutoscaler** bounds and historical replica behavior for autonomous scaling policies. See [Node recommendations roadmap](../architecture/node-recommendations-roadmap.md#tier-overview).

## Fleet instance type suggestions (Tier 1)

When a node is **CPU-stranded** or **memory-stranded**, the engine can suggest a different **instance type that already exists in the same cluster** (no external catalog):

| Stranded | Suggestion logic |
|----------|------------------|
| CPU (memory-heavy workload) | Instance type in cluster with **lower** allocatable CPU:memory ratio |
| Memory (CPU-heavy workload) | Instance type in cluster with **higher** allocatable CPU:memory ratio |

Response fields: `suggested_instance_type`, `instance_type_reason`. Empty when no better in-cluster type exists (directional stranded notifications still apply).

### Future: External Cloud Instance Catalog

Tier 1 instance type hints compare **types already observed in your cluster** only. A future enhancement would integrate external pricing catalogs (AWS EC2, Azure VM, GCP Compute pricing APIs) to:

- Recommend instance types **not yet present** in the cluster
- Factor in on-demand and reserved pricing for cost-optimized family/size changes

No delivery timeline — tracked in [node recommendations roadmap](../architecture/node-recommendations-roadmap.md#future-work--full-cloud-catalog-tier-2).

## API

### List and filters

```http
GET /api/cost-management/v1/recommendations/openshift/nodes
GET /api/cost-management/v1/recommendations/openshift/nodes?format=csv
```

When `RBAC_ENABLE=true`, the list is scoped by Insights permissions on
`openshift.cluster` and `openshift.node` (same as detail and CSV export). Users
without access to a cluster or node see an empty list rather than partial rows.

| Parameter | Description |
|-----------|-------------|
| `filter[cluster]` | Cluster UUID |
| `filter[node]` | Node name (exact) |
| `filter[term]` | `short`, `medium`, `long` |
| `filter[engine]` | `cost`, `performance` |
| `filter[is_underutilized]` | `true` / `false` |
| `filter[is_overcommitted]` | `true` / `false` |
| `filter[idle_state]` | `active`, `idle`, `zombie` (comma-separated) |
| `filter[stranded_resource]` | `cpu`, `memory`, `none` |
| `filter[instance_type]` | Exact instance type |
| `filter[machineset_name]` | Exact MachineSet name |
| `filter[tag:<key>]` | Tag value filter when `ROS_TAGS_ENABLED=true` (node must host workloads with matching namespace tags) |
| `order_by` | `estimated_monthly_savings` (default) or `node` |
| `limit` / `offset` | Pagination |

Legacy flat params (`?cluster_uuid=`, `?node=`, `?term=`, `?engine=`) still work.

### Single-node detail

```http
GET /api/cost-management/v1/recommendations/openshift/nodes/{node}?filter[cluster]=UUID
```

Registered detail route — same nested `recommendation_terms` payload as list.

List-filter equivalent:

```http
GET /api/cost-management/v1/recommendations/openshift/nodes?filter[cluster]=UUID&filter[node]=worker-0&limit=1
```

Deprecated list alias: `GET .../nodes/utilization`.

### Example (abbreviated)

```json
{
  "meta": { "count": 12, "currency": "USD" },
  "data": [{
    "node": "worker-3.example.com",
    "cluster_uuid": "...",
    "instance_type": "m5.2xlarge",
    "machineset_name": "worker",
    "pod_count": 42,
    "pod_capacity": 110,
    "pod_scheduling_headroom": 0.618,
    "suggested_instance_type": "m5.large",
    "instance_type_reason": "Memory-heavy workload; lower CPU:memory ratio than m5.2xlarge",
    "recommendation_type": "cpu_memory_utilization",
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

Canonical path:

```http
GET /api/cost-management/v1/recommendations/openshift/settings/node
PUT /api/cost-management/v1/recommendations/openshift/settings/node
DELETE /api/cost-management/v1/recommendations/openshift/settings/node
```

Deprecated alias (returns `Deprecation: true` and a `Link` header to `/settings/node`):

```http
GET/PUT/DELETE .../settings/thresholds?recommendation_type=node
```

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
| `zombie_cpu_p95_mc` | 200 | Zombie: CPU P95 below this (millicores) with few pods |
| `zombie_max_pods` | 5 | Zombie: max running pods |
| `idle_cpu_util_pct` | 10 | Idle: CPU util % of allocatable (0–100) |
| `idle_mem_util_pct` | 10 | Idle: memory util % of allocatable (0–100) |
| `idle_max_pods` | 10 | Idle: max running pods |
| `pod_headroom_consolidation_gate` | 0.15 | Min headroom before consolidation is suppressed |
| `pod_headroom_notification_threshold` | 0.10 | Headroom below this emits notification **74** |

Idle and zombie thresholds are tenant-configurable via the Settings API (not env-only).
Env vars: `ROS_NODE_*` — see [Configurability](../architecture/configurability.md#node).

## MachineSet aggregation

`GET /api/cost-management/v1/recommendations/openshift/machinesets` groups existing per-node recommendations by OpenShift MachineSet name (aggregation over `node_recommendations`; no separate engine table yet).

```
GET /api/cost-management/v1/recommendations/openshift/machinesets
```

**`machineset_name` availability:** Requires the Machine API (`machine.openshift.io/v1beta1`). Present on IPI-installed clusters and other environments with MachineSet management. **Not** available on bare-metal UPI, single-node OpenShift (SNO), or non-OpenShift Kubernetes — `machineset_name` is omitted and `GET .../machinesets` returns empty results. Fleet consolidation still works via `instance_type` or capacity-based grouping; per-node recommendations are unaffected. MachineSet-less clusters are typically small or manually managed, where fleet consolidation is less relevant.

| Field | Meaning |
|-------|---------|
| `current_node_count` | Nodes in the MachineSet with a recommendation row |
| `excess_nodes` | Sum of `node_count_reduction` (fleet consolidation assigns 1 per removable node) |
| `recommended_node_count` | `current_node_count - excess_nodes` |
| `total_monthly_savings_usd` | Sum of member node savings (USD) |
| `avg_cpu_utilization` / `avg_memory_utilization` | Average P95 util across members |

Filters: `filter[cluster]`, `filter[machineset_name]` (exact or `*` wildcard), `filter[term]` (default `medium`). RBAC matches the node list API (`openshift.cluster`, `openshift.node`).

Replica count and cross-cloud instance-family changes still require the planned catalog + `machineset` engine work — see [roadmap](../architecture/node-recommendations-roadmap.md).

## Future enhancements

Tier 1 (this document) is **implemented**. Planned work is grouped by tier below. Full design, prerequisites, effort estimates, and schema notes:
**[Node recommendations roadmap — Tier 2 & Tier 3](../architecture/node-recommendations-roadmap.md)**.

### Intentionally out of scope (Tier 1)

| Item | Rationale |
|------|-----------|
| **Business hours for nodes** | Nodes are always-on infrastructure; `idle_state` (`active` / `idle` / `zombie`) covers decommissioning without schedule complexity. Container and namespace recommendations retain business-hours support. |

### Tier 2 (planned)

Phased as **Tier 2a** (no cloud catalog) then **Tier 2b** (catalog). See
[MachineSet recommendations (planned)](../planned-features/machineset-recommendations.md).

**Tier 2a:** `machineset` engine plugin, `machineset_recommendations` table, replica
count recommendations, `GET /machinesets/{name}` detail, history/trends, MachineSet
confidence, heterogeneous fleet detection, fleet-health notifications, PDB caveat (code **4**).

**Tier 2b:** Cloud instance catalog (AWS/Azure/GCP), instance family/size recommendations,
cost comparison, deprecated-instance handling (codes **23**, **24**).

**Also planned (later Tier 2 scope):** PDB/scheduling-aware consolidation (safe for
auto-execution), node recommendation history API, GPU-aware node consolidation.

### Tier 3 (future)

- MachineAutoscaler optimization (notification codes **14**, **16**, **17**; code **75** reserved for future `minReplicas` signal — code **15** is **`NODE_IDLE`** for nodes)
- Autonomous scaling recommendations
- Operator: MachineSet replica time-series, autoscaler CR metrics

## History and quality

Node history and quality endpoints do not exist. History and quality are available
for container recommendations only (`GET .../history`, `GET .../quality`). Per-node
trend data is not exposed via a dedicated history API; use list/detail metrics and
notifications instead.

## Related

- [MachineSet recommendations (planned)](../planned-features/machineset-recommendations.md) — Tier 2 product direction and Tier 2a/2b phasing
- [Node recommendations roadmap (Tier 2 & 3)](../architecture/node-recommendations-roadmap.md) — MachineSet and MachineAutoscaler planned work
- [Dual Engine](dual-engine.md) — Cost vs performance trade-offs
- [Savings Estimations](savings-estimations.md) — Fleet-level node savings totals
- [GPU Time-Slicing](gpu-time-slicing.md) — Separate node-level GPU feature
