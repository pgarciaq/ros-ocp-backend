# Idle and zombie detection

ROS classifies workloads that consume resources without meaningful utilization so operators can reclaim cost. Configuration is per-tenant via the idle-detection settings API; classification runs during the daily recommendation pipeline.

## States

| State | Meaning |
|-------|---------|
| **active** | Workload shows utilization relative to requests (or burst activity). |
| **idle** | Sustained low CPU and/or memory utilization vs configured percentages. |
| **zombie** | Near-zero CPU: P95 and peak below zombie thresholds over the observation window. |

## Container classification

1. **Exclusions** — Namespaces (globs) and workload types in settings are never classified idle/zombie.
2. **Minimum observation** — Requires `minimum_observation_days` of digest data (default 14).
3. **Burst guard** — If peak CPU > `burst_ratio` × P95 CPU, state stays **active** (protects CronJobs and bursty jobs).
4. **Zombie** — P95 CPU < `zombie_cpu_millicores` **and** peak CPU < `zombie_peak_millicores` (defaults 1 mc / 10 mc; configurable via settings).
5. **Idle** — Otherwise, if CPU utilization < `cpu_utilization_percent`% of request **or** memory utilization < `memory_utilization_percent`% of request.

Peak and P95 come from daily container digests. `idle_since` is the first day the predicate held; `idle_duration_days` is days since then at classification time.

## Namespace idle

After container and GPU rows exist, namespace plugin aggregates: a namespace is **idle** when every non-excluded container is idle or zombie and every GPU row is non-active, with at least one container present.

Filter list results: `filter[idle_state]=idle` on `GET /recommendations/openshift/namespace`.

## Node idle

Nodes use utilization from node digests and pod counts with zombie CPU thresholds from idle-detection settings (aligned with container zombie defaults unless overridden by node threshold settings).

Filter: `filter[idle_state]` on `GET /recommendations/openshift/nodes`.

## GPU idle

GPU classification uses DCGM basis points (0–10000 = 0–100%):

- **Zombie** — P95 SM active and P95 DRAM active both below zombie basis points (admin default 100 = 1%).
- **Idle** — P95 SM and P95 DRAM below `gpu_sm_active_basis_points` and `gpu_dram_active_basis_points` (default 500 = 5%).

Persisted on `recommendation_sets` as `gpu_idle_state`, `gpu_idle_since`, `gpu_idle_duration_days`, `gpu_estimated_waste_cents`. Exposed on the container `gpu` map in list/detail responses.

Filters:

- Container list: `filter[gpu_idle_state]=zombie,idle` (with `filter[has_gpu]=true` as needed).
- MIG list: `filter[gpu_idle_state]` on `GET /recommendations/openshift/gpu/mig`.

## Configurable thresholds (defaults)

| Field | Default | Range (API) |
|-------|---------|-------------|
| `enabled` | true | boolean |
| `cpu_utilization_percent` | 2 | 1–50 |
| `memory_utilization_percent` | 5 | 1–50 |
| `burst_ratio` | 10 | 2–100 |
| `minimum_observation_days` | 14 | 3–90 |
| `gpu_sm_active_basis_points` | 500 | 100–5000 |
| `gpu_dram_active_basis_points` | 500 | 100–5000 |
| `zombie_cpu_millicores` | 1 | 0–100 |
| `zombie_peak_millicores` | 10 | 0–1000 |

Exclusions: `exclusions.namespaces` (globs), `exclusions.workload_types` (Deployment, StatefulSet, etc.).

Environment variables can lock fields (see `ROS_IDLE_*` in deployment docs). **Advanced:** incorrect `zombie_cpu_millicores` or `zombie_peak_millicores` may cause false zombie classification.

## Notification codes

| Code | Name | When |
|------|------|------|
| 5 | IDLE_WORKLOAD | Container idle |
| 8 | ABANDONED_WORKLOAD | Legacy abandoned (zero usage); see `DetectAbandoned` |
| 15 | NODE_IDLE | Node idle/zombie |
| 26 | GPU_IDLE | GPU idle |

## Waste calculation

For containers and GPUs in **idle** or **zombie**, `estimated_monthly_waste` / `gpu_estimated_waste_cents` reflect **full monthly cost** of the workload (not rightsizing delta). Rightsizing `estimated_monthly_savings` is cleared on idle container rows.

## Settings API

| Method | Path |
|--------|------|
| GET | `/api/cost-management/v1/recommendations/openshift/settings/idle-detection` |
| PUT | `/api/cost-management/v1/recommendations/openshift/settings/idle-detection` |
| DELETE | `/api/cost-management/v1/recommendations/openshift/settings/idle-detection` |

PUT/DELETE trigger async threshold recalculation for **container**, **gpu**, **namespace**, and **node** plugins.

## List API examples

```http
GET /api/cost-management/v1/recommendations/openshift?filter[idle_state]=zombie,idle
GET /api/cost-management/v1/recommendations/openshift?filter[gpu_idle_state]=idle&filter[has_gpu]=true
GET /api/cost-management/v1/recommendations/openshift/savings-summary?group_by[idle_state]=*
GET /api/cost-management/v1/recommendations/openshift?order_by=idle_duration_days&order_how=desc
```

## Related

- Feature design: [Idle detection](../features/idle-detection.md)
- Query parameters: [Query parameters](query-parameters.md)
