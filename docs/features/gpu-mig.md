# GPU MIG Recommendations (internal)

Public docs: [docs-site/features/gpu-mig.md](../../docs-site/features/gpu-mig.md).

## Overview

Per-container MIG profile recommendations on MIG-capable NVIDIA GPUs. List endpoint
returns rows where `recommended_gpu_profile` is set and not `full_gpu`.

| Item | Value |
|------|-------|
| API | `GET /api/cost-management/v1/recommendations/openshift/gpu/mig` |
| Handler | [`GetGPUMIGRecommendations`](../../internal/api/handlers_gpu_mig.go) |
| Engine | [`QueryGPURecommendations`](../../internal/engine/gpu_query.go), MIG bin-pack in [`gpu_recommender.go`](../../internal/engine/gpu_recommender.go) |
| Catalog | [`gpu_catalog.yaml`](../../internal/engine/gpu_catalog.yaml) |

Uses **recommendation terms** (`short` / `medium` / `long`), not `filter[engine]=cost|performance`.
Savings: container detail only (`GET .../recommendations/openshift/{uuid}` → `gpu.{term}`).

## Flow

1. Daily [`gpu_container_digests`](../../internal/testutil/fixtures.go) (DCGM aggregates).
2. Classification + idle detection → [`RecommendGPU`](../../internal/engine/gpu_recommender.go).
3. MIG profile selection (P98 FB × `fb_headroom_factor`).
4. List filters/sorts/paginates in memory in the handler.

## API (list)

| Filter | Alias | Notes |
|--------|-------|-------|
| `filter[cluster]` | `cluster`, `cluster_uuid` | RBAC + unknown cluster → empty 200 |
| `filter[project]` | `namespace` | Comma-separated OR |
| `filter[gpu_idle_state]` | — | `active`, `idle`, `zombie` |
| `filter[tag:<key>]` | — | Requires `ROS_TAGS_ENABLED` |

`order_by`: `cluster_uuid`, `namespace`, `workload`, `container`, `term`, `gpu_model`, `confidence`.
`limit` / `offset` (default 100, max 1000). `format=csv` or `Accept: text/csv`.

## Settings

| Endpoint | Purpose |
|----------|---------|
| `GET/PUT/DELETE .../settings/gpu` | GPU thresholds ([`threshold_settings_routes.go`](../../internal/api/threshold_settings_routes.go)) |
| `GET/PUT/DELETE .../settings/terms?recommendation_type=gpu` | Term windows |

Defaults include `idle_threshold` (0.02), `fb_headroom_factor` (1.20), `mig_fb_percentile` (0.98).
See [configurability](../architecture/configurability.md#gpu).

## Notifications

Container detail may include GPU advisory codes **10**, **26–28** (MIG right-sizing / idle).
Time-slicing uses code **36** on node recommendations.

Reference: [notification-codes](../architecture/notification-codes.md).

## RBAC

- Clusters: `openshift.cluster` — [`filterClustersByRBAC`](../../internal/api/handlers_gpu_mig.go).
- Nodes: `openshift.node` — [`filterGPUMIGEntriesByRBAC`](../../internal/api/handlers_gpu_mig.go).
- No ROS permissions → **403** (identity middleware).
- `filter[cluster]` for a cluster outside RBAC → **empty list (200)**.

Integration coverage: [`handlers_gpu_mig_integration_test.go`](../../internal/api/handlers_gpu_mig_integration_test.go).

## Test Data Generation

Generate OCP payloads with GPU ROS metrics using [nise](https://github.com/project-koku/nise):

```bash
nise report ocp \
  --static-report-file /path/to/ocp_static_data.yml \
  --ocp-cluster-id <cluster_uuid> \
  --ros-ocp-info \
  --write-monthly
```

Use a static YAML that defines GPU-enabled workloads (A100/H100 nodes, containers with low SM/DRAM
utilization for MIG candidates). The `--ros-ocp-info` flag emits container-level ROS CSVs
(`ocp_ros_usage.csv`, `ocp_ros_namespace_usage.csv`) required by the ros-ocp-backend processor.

GPU usage CSVs (`ocp_gpu_usage.csv` from cost ingestion) should include columns used for
classification and MIG sizing:

| Column | Role |
|--------|------|
| `gpu_model` | NVIDIA model name (must match [`gpu_catalog.yaml`](../../internal/engine/gpu_catalog.yaml)) |
| `gpu_uuid` | Device identity |
| `instance_name` | MIG instance / profile when partitioned |
| `utilization` | Utilization signal (with DCGM-derived SM/DRAM/FB fields in ROS GPU digests) |

Package typed monthly files into a tarball with `manifest.json` (`start`/`end` dates, `files`,
`resource_optimization_files`) and upload via ingress. See
[validating-native-engine](../testing/validating-native-engine.md) for on-prem E2E flow.

## Plugin Management

GPU MIG routes are provided by the **`gpu`** recommendation plugin. Enable or disable via:

| Variable | Behavior |
|----------|----------|
| `ROS_ENABLED_PLUGINS` | When non-empty, allowlist only (e.g. `container,gpu,node`). Omit `gpu` to disable. |
| `ROS_DISABLED_PLUGINS` | When allowlist is empty, subtract plugins from the default native set. |

When the `gpu` plugin is disabled, `GET .../recommendations/openshift/gpu/mig` and related
`/gpu/*` paths return **404** with `plugin 'gpu' is not enabled` (route guards), not a container
detail 400. GPU threshold settings (`GET .../settings/gpu`) follow the same plugin gating.

See [Plugin architecture](../architecture/plugin-architecture.md) for registry, phases, and
trait design. Integration coverage:
[`handlers_gpu_mig_integration_test.go`](../../internal/api/handlers_gpu_mig_integration_test.go).

## Related

- [GPU time-slicing](gpu-time-slicing.md) — node-level; mutually exclusive with MIG candidates
- [GPU classification](../architecture/gpu-classification.md)
- Koku cost report: `GET .../reports/openshift/gpu/mig_profiles/` (spend, not ROS recommendations)
