# GPU Time-Slicing Recommendations (internal)

Public docs: [docs-site/features/gpu-time-slicing.md](../../docs-site/features/gpu-time-slicing.md).

## Overview

Node-level recommendations to share underutilized physical GPUs across containers via
`nvidia.com/gpu.replicas` (NVIDIA device-plugin time-slicing). One row per
node × GPU model × term when the engine emits a recommendation.

| Item | Value |
|------|-------|
| API | `GET /api/cost-management/v1/recommendations/openshift/gpu/timeslicing` |
| Handler | [`GetNodeRecommendations`](../../internal/api/handlers_node_recs.go) |
| Engine | [`ComputeNodeTimeslicingRec`](../../internal/engine/gpu_timeslicing.go) |
| Table | `node_recommendations` (type `gpu_time_slicing`) |

Uses **recommendation terms** (`short` / `medium` / `long`), not `filter[engine]=cost|performance`.
Savings: list (`total_node_savings`, `savings_per_gpu` as `SavingsObject`; currency in `meta.currency`) and container detail
(`estimated_monthly_timeslicing_savings` on `gpu.{term}`).

## Flow

1. Daily [`gpu_container_digests`](../../internal/testutil/fixtures.go) (DCGM aggregates, per container × node).
2. Partition by node + GPU model; classify candidates vs impacted workloads.
3. Majority check, replica math (`ceil(1/peak_util)` clamped to min/max replicas).
4. Persist/query node recommendations; handler filters, sorts, paginates, optional CSV.

## API (list)

| Filter | Alias | Notes |
|--------|-------|-------|
| `filter[cluster]` | `cluster`, `cluster_uuid` | RBAC + unknown cluster → empty 200 |
| `node_name` | — | Exact match (case-insensitive) |
| `gpu_model` | `filter[gpu_model]` | Substring match |
| `filter[gpu_idle_state]` | — | Not on list; idle containers excluded in engine |
| `filter[tag:<key>]` | `tag=key:value` | Requires `ROS_TAGS_ENABLED` |
| `term` | — | Term window filter |

`order_by`: `node_name`, `cluster_uuid`, `gpu_model`, `recommended_replicas`, `confidence`,
`total_node_savings` (alias: `total_node_savings_usd`). `order_how`: `asc` / `desc`.
`limit` / `offset` (default 100, max 1000). `format=csv` or `Accept: text/csv`.

No `filter[project]` — node-level scope only.

## Settings

| Endpoint | Purpose |
|----------|---------|
| `GET/PUT/DELETE .../settings/gpu` | GPU + time-slicing thresholds ([`threshold_settings.go`](../../internal/engine/threshold_settings.go)) |
| `GET/PUT/DELETE .../settings/terms?recommendation_type=gpu` | Term windows |

Time-slicing keys on `/settings/gpu`: `timeslicing_min_replicas`, `timeslicing_max_replicas`,
`timeslicing_majority_threshold`, `timeslicing_base_penalty`, `timeslicing_impacted_weight`,
`node_freshness_days`. Classification keys (`idle_threshold`, `underutilized_sm_threshold`, …)
drive which containers become candidates.

## Notifications

List rows and candidate containers include notification code **36**
(`NotifGPUTimeSharingCandidate` / `GPU_TIMESLICING_CANDIDATE`).

Reference: [notification-codes](../architecture/notification-codes.md).

## RBAC

- Clusters: `openshift.cluster` — handler cluster allowlist.
- Nodes: `openshift.node` — node-scoped filtering on list rows.
- No ROS permissions → **403** (identity middleware).
- `filter[cluster]` for a cluster outside RBAC → **empty list (200)**.

Integration coverage: [`handlers_node_recs_integration_test.go`](../../internal/api/handlers_node_recs_integration_test.go).

## Plugin enablement

Routes are served by the **`gpu`** plugin (`ROS_ENABLED_PLUGINS` / `ROS_DISABLED_PLUGINS`).
Omit `gpu` → **404** on `/gpu/*`, not an empty list.

## Test data generation

```bash
nise report ocp \
  --static-report-file /path/to/ocp_static_data.yml \
  --ocp-cluster-id <cluster_uuid> \
  --ros-ocp-info \
  --write-monthly
```

Use GPU nodes with multiple underutilized containers (low SM/DRAM on T4/L4-class hardware).
Namespace label `cost_management_optimizations: "true"` for ROS profiling CSVs.

On-prem E2E: `cost-onprem-chart/tests/data/nise_templates/ocp_report_gpu_timeslicing.yml`.

## Summary vs list counts

`GET .../gpu` → `timeslicing.count` = telemetry coverage (node×model triples).
`GET .../gpu/timeslicing` → `meta.count` = actionable recommendations (N ≥ M). See public doc.

## Related

- [GPU MIG (internal)](gpu-mig.md)
- [Validating native engine](../../docs-site/testing/validating-native-engine.md)
