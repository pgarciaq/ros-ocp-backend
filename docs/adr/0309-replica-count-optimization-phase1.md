# 0309 — Replica Count Optimization — Phase 1

**Status:** Accepted
**Date:** 2026-07-01
**Domain:** Engine / Algorithm
**Phase:** 15+
**Issue:** [#98](https://github.com/pgarciaq/ros-ocp-backend/issues/98)

## Context

ROS-OCP currently recommends per-container CPU and memory request/limit sizing
but does not advise on the total number of replicas a workload should run.
Many Deployments and StatefulSets are over-replicated — the workload's aggregate
resource usage could be served by fewer replicas once per-replica sizing is
optimized.

All required data already exists in the database:
- `desired_replicas` / `available_replicas` on `recommendation_sets`
- Per-replica P95 CPU and memory usage from `daily_container_digests`
- `RecCPURequestMC` / `RecMemRequestKiB` computed by the sizing engine

## Decision

### Formula

```
min_replicas_cpu = ceil((cpu_usage_p95_mc × current_replicas) / (rec_cpu_request_mc × target_util))
min_replicas_mem = ceil((mem_usage_p95_kib × current_replicas) / (rec_mem_request_kib × target_util))
recommended_replicas = max(min_replicas_cpu, min_replicas_mem)
recommended_replicas = max(recommended_replicas, minimum_floor)
```

### Target Utilization

Default 70%, configurable via `ROS_REPLICA_TARGET_UTILIZATION_PCT` (env var)
or the Settings API `replica_target_utilization_pct` (tenant override).
Validated to 10–95% range. Follows the existing three-tier settings precedence
(env var > tenant PUT > default).

### Minimum Floor

- **Deployment-like workloads:** 2 replicas (HA guarantee)
- **StatefulSet:** 1 replica (may legitimately run as singleton)

### Scope

- **Always emit** `recommended_replicas` even when equal to the current count.
- **Skip DaemonSets** entirely (not replica-scalable).
- **Require ≥ 2 current replicas** to activate (single-replica workloads skipped).
- **Require positive RecCPURequestMC and RecMemRequestKiB** (zero = insufficient data).

### Confidence Heuristic

Based on per-pod load symmetry, using the P50/P95 CPU spread as a proxy:

| Workload Type | Spread Value | Confidence |
|---------------|-------------|------------|
| Deployment    | (any)       | high       |
| StatefulSet   | < 0.25      | high       |
| StatefulSet   | < 0.40      | medium     |
| StatefulSet   | ≥ 0.40      | low        |

Gated on stable pod count (`PodCountMin == PodCountMax && PodCountMin >= 2`);
unstable counts → medium.

Rationale: Deployments have symmetric pods by design, so aggregate-based
division is safe. StatefulSets may have leader/follower asymmetry where one pod
uses significantly more resources; high P50/P95 spread signals this risk.

### Savings Extension

When `recommended_replicas < current_replicas`:
- Per-replica savings use `recommended_replicas` instead of `current_replicas`
- Additional savings from freed replicas: `freed_replicas × per_replica_resource_cost`

### Database Schema

Three new nullable columns on `recommendation_sets`:

| Column | Type | Description |
|--------|------|-------------|
| `recommended_replicas` | INTEGER | Optimal replica count |
| `replica_confidence` | TEXT | high / medium / low |
| `replica_explanation` | TEXT | Human-readable rationale |

### API Response

New `replica_optimization` object in the container detail response:

```json
{
  "replica_optimization": {
    "recommended_replicas": 3,
    "confidence": "high",
    "explanation": "Workload can be consolidated from 5 to 3 replicas at 70% target utilization."
  }
}
```

## Consequences

### Positive

- Immediate value for over-replicated workloads (common after scaling events)
- Uses existing data — no new operator metrics or Prometheus queries required
- Confidence indicator helps users gauge recommendation reliability
- Savings estimate now includes both per-replica sizing AND replica count reduction

### Negative

- Phase 1 uses aggregate P95 across all pods — does not detect per-pod
  asymmetry beyond the P50/P95 heuristic. Phase 2 will add a per-pod CV
  column to `daily_container_digests` for true per-pod variance detection.
- The min-floor of 2 for Deployments is conservative; some workloads can
  safely run as singletons. Phase 2 may refine this based on PDB analysis.

## Phase 2 (Future)

- Per-pod coefficient of variation (CV) column in `daily_container_digests`
- PDB-aware minimum floor (instead of hardcoded 2)
- Memory-dominant workload detection for more nuanced recommendations
