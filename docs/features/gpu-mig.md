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

## Related

- [GPU time-slicing](gpu-time-slicing.md) — node-level; mutually exclusive with MIG candidates
- [GPU classification](../architecture/gpu-classification.md)
- Koku cost report: `GET .../reports/openshift/gpu/mig_profiles/` (spend, not ROS recommendations)
