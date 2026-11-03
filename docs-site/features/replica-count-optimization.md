# Replica Count Optimization

> **Last verified:** 2026-11-03

!!! info "Quick Facts"
    **Recommendation type:** Optimal replica count for Deployments and StatefulSets  
    **Phase:** 1 (resource-based) **shipped**; phases 2–3 planned  
    **Input data:** Per-replica CPU/memory usage percentiles and `desired_replicas` (already collected — no new operator queries)  
    **Output:** `recommended_replicas` per workload per term, with confidence and explanation  
    **API:** `replica_optimization` object in the container detail response; `recommended_replicas` in list CSV exports  
    **Applicable workloads:** Deployment, StatefulSet  
    **Not applicable:** DaemonSet (count determined by cluster topology), bare Pods, CronJobs

---

## What it does

Per-container right-sizing sizes individual containers but treats the
replica count as a fixed input. Replica Count Optimization adds the
horizontal dimension: given a workload's per-replica utilization profile and
the recommended per-replica sizing, it recommends the **optimal number of
replicas**.

Removing an entire pod saves every resource dimension at once — CPU,
memory, network, and associated storage. For over-replicated workloads
(3–5 replicas where 2 would satisfy both performance and availability),
replica reduction is often the highest-impact optimization available.

Designed in [ADR-0309](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0309-replica-count-optimization-phase1.md)
and shipped with issue [#98](https://github.com/pgarciaq/ros-ocp-backend/issues/98).

---

## How it works (Phase 1 — shipped)

### Capacity model

Given P95 per-replica usage at the current replica count, compute the
minimum replicas needed to stay under a target utilization ceiling:

```
min_replicas_cpu = ceil((cpu_usage_p95_mc × current_replicas) / (rec_cpu_request_mc × target_util))
min_replicas_mem = ceil((mem_usage_p95_kib × current_replicas) / (rec_mem_request_kib × target_util))
recommended_replicas = max(min_replicas_cpu, min_replicas_mem)
recommended_replicas = max(recommended_replicas, minimum_floor)
```

The final recommendation is the **maximum** of the CPU-derived and
memory-derived counts, never below the workload-type floor.

### Target utilization

Default **70%**, configurable via `ROS_REPLICA_TARGET_UTILIZATION_PCT` or
the Settings API (`replica_target_utilization_pct` tenant override),
validated to the **10–95%** range. Follows the standard three-tier settings
precedence (env var > tenant PUT > default).

### Minimum floors

- **Deployments:** never recommend fewer than **2** replicas (HA guarantee)
- **StatefulSets:** never fewer than **1** (may legitimately run as a singleton)

### Scope rules

- **Skip DaemonSets** entirely — not replica-scalable (their optimization
  belongs to [Node Consolidation](node-recommendations.md))
- **Require ≥ 2 current replicas** — single-replica workloads are skipped
- **Require positive recommended requests** — a zero request means
  insufficient data, so no replica advice is emitted
- **Always emit** `recommended_replicas`, even when it equals the current
  count (the explanation then reads "current replica count is optimal")

### Confidence heuristic

Confidence is based on per-pod load symmetry, using the P50/P95 CPU spread
as the proxy:

| Workload type | Spread | Confidence |
|---------------|--------|------------|
| Deployment | any | high |
| StatefulSet | < 0.25 | high |
| StatefulSet | < 0.40 | medium |
| StatefulSet | ≥ 0.40 | low |

Gated on stable pod count (`pod_count_min == pod_count_max ≥ 2`); unstable
counts downgrade to medium. Rationale: Deployments have symmetric pods by
design; StatefulSets may have leader/follower asymmetry where one pod
carries significantly more load, and a high P50/P95 spread signals that
risk.

### Savings

When `recommended_replicas < current_replicas`, the freed replicas' full
resource cost is added to the workload's estimated savings (and the
per-replica savings multiply by the recommended count, not the current
one) — see
[`savings.go`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/librobne/container/savings.go).
Replica-reduction savings are folded into the aggregate
`estimated_monthly_savings` the API serves; they are not a separate field.

---

## API surface

The container detail response carries a `replica_optimization` object:

```json
"replica_optimization": {
  "recommended_replicas": 3,
  "confidence": "high",
  "explanation": "Workload can be consolidated from 5 to 3 replicas at 70% target utilization (P95 CPU: 200 mc, P95 mem: 2048 KiB per replica)."
}
```

List CSV exports include the `recommended_replicas` column.

---

## Known limitation: autoscaler-managed workloads

Replica-count recommendations are **not currently suppressed** for
workloads managed by an autoscaler (HPA today; KEDA-aware detection is
tracked in [#784](https://github.com/pgarciaq/ros-ocp-backend/issues/784)).
If you consume recommendations with automation, filter replica-count
advice for autoscaler-managed workloads yourself.

---

## Planned phases (not yet implemented)

### Phase 2: Traffic-aware

Adds application-level traffic metrics (requests per second from service
mesh or Prometheus) or infrastructure-level inference, enabling
traffic-proportional recommendations rather than pure resource analysis.
Requires new operator queries and a traffic correlation model — tracked
in [#795](https://github.com/pgarciaq/ros-ocp-backend/issues/795).

### Phase 3: HPA configuration advice

Outputs recommended HPA parameters (`minReplicas`, `maxReplicas`,
`targetCPUUtilization`) from observed patterns. Requires Phase 2 data plus
HPA awareness in the engine — tracked in
[#796](https://github.com/pgarciaq/ros-ocp-backend/issues/796); complements
the planned
[HPA Recommendations](../planned-features/hpa-recommendations.md) feature.

---

## Limitations

- **Phase 1 assumes uniform per-replica usage.** Skewed distributions
  (leader/follower, primary/secondary) may produce incorrect
  recommendations; the StatefulSet confidence heuristic flags this risk,
  and Phase 2 traffic analysis can account for it.
- **Stateful workloads with per-replica data affinity** (each replica owns
  a data shard) cannot be safely consolidated even at low usage — treat
  low-confidence StatefulSet recommendations accordingly.
- **Advisory only.** No actuation: no HPA/VPA modification, no scaling.
- **No availability modeling.** The floors enforce a general HA guarantee,
  but application-specific requirements (e.g., quorum) are the user's to
  apply.

---

## Applicable entity types

| Workload type | Applicable | Rationale |
|---------------|------------|-----------|
| Deployment | Yes | Standard replica controller; most common target |
| StatefulSet | Yes, with caveats | Possible, but data affinity and asymmetry lower confidence |
| DaemonSet | No | One pod per node; count is cluster topology |
| DeploymentConfig | No | Deprecated OpenShift resource |
| Bare Pod | No | No replica controller |
| CronJob | No | Concurrency is a scheduling concern |

---

## Related

- [ADR-0309: Replica Count Optimization — Phase 1](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0309-replica-count-optimization-phase1.md)
- [ADR-0042: Use desired_replicas over pod_count_avg](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0042-desired-replicas-over-pod-count-avg.md)
- [Savings Estimations](savings-estimations.md)
- [Container Right-Sizing](container-recommendations.md)
- [Node Consolidation](node-recommendations.md)
