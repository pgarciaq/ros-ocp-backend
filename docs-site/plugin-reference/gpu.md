# gpu

Package: [`internal/plugins/gpu`](../../internal/plugins/gpu/)

**GPU right-sizing** — analyzes NVIDIA DCGM metrics from container ROS CSVs, classifies utilization (compute/memory-bound, idle, mixed), and recommends MIG profiles, time-slicing, and idle remediation.

## Plugin metadata

| Property | Value |
|----------|-------|
| Name | `gpu` |
| Phase | 1 (Produce) + API enrich |
| Priority | 20 |
| CSV types | (none — `IngestHook` after `container`) |
| Retention tables | `gpu_container_digests` |

## Traits

| Trait | Supported |
|-------|-----------|
| CSVIngestor | No |
| IngestHook | Yes — after `container` CSV; upserts `gpu_container_digests` |
| APIEnricher | Yes — decorates container list/detail `gpu` map |
| APIProvider | Yes — fleet summary, time-slicing, MIG list |
| TermProvider | Yes — short/medium/long (max 90 days) |

## What it does

GPU metrics piggyback on container ingestion (DCGM SM/DRAM/FB profiling, model, MIG profile). The engine classifies workloads and exposes:

- **Container enrichment** — `gpu` block on `GET /recommendations/openshift` list/detail
- **MIG** — smallest profile fit per workload (`GET .../gpu/mig`)
- **Time-slicing** — node-level replica guidance (`GET .../gpu/timeslicing`)
- **Fleet summary** — aggregated GPU inventory (`GET .../gpu`)

## Key settings

Configurable thresholds via the Settings API (per-org overrides; `ROS_GPU_*` env locks):

```
GET /api/cost-management/v1/recommendations/openshift/settings/gpu
PUT /api/cost-management/v1/recommendations/openshift/settings/gpu
DELETE /api/cost-management/v1/recommendations/openshift/settings/gpu
```

Typical fields include SM/DRAM active basis points, MIG headroom, and classification bands. Resolution order: **Settings API** → **`ROS_GPU_*`** → compiled defaults.

See [Configurability](../architecture/configurability.md) (GPU section) and [GPU classification](../architecture/gpu-classification.md).

**Enablement:** Include `gpu` in `ROS_ENABLED_PLUGINS`. Routes and enrichment are omitted when disabled.

## Idle detection

Persisted on `recommendation_sets` and exposed on container/MIG responses:

| Field | Meaning |
|-------|---------|
| `gpu_idle_state` | `active`, `idle`, or `zombie` |
| `gpu_idle_since` | First day the idle/zombie predicate held |
| `gpu_idle_duration_days` | Days in current idle/zombie state |

Classification uses DCGM basis points (defaults: idle at 5% SM/DRAM, zombie at 1%). Filters:

- Container list: `filter[gpu_idle_state]=idle,zombie` (often with `filter[has_gpu]=true`)
- MIG list: `filter[gpu_idle_state]` on `GET .../gpu/mig`

### Tag filtering

MIG and time-slicing list endpoints support `filter[tag:<key>]=<value>` when `ROS_TAGS_ENABLED=true` (namespace label scope). Syntax matches other ROS list APIs — see [Query parameters](query-parameters.md).

See [Idle / zombie detection](idle-detection.md#gpu-idle).

## MIG support

For MIG-capable GPUs, the engine maps P98 framebuffer usage (with headroom) to standard profiles (`1g.5gb` through `7g.40gb`, or `full_gpu`). Workloads that are not MIG candidates remain on full-GPU recommendations.

Feature doc: [GPU MIG recommendations](../features/gpu-mig.md). Catalogs: [GPU catalogs](../architecture/gpu-catalogs.md).

## Endpoints

```
GET /api/cost-management/v1/recommendations/openshift/gpu
GET /api/cost-management/v1/recommendations/openshift/gpu/mig
GET /api/cost-management/v1/recommendations/openshift/gpu/timeslicing
GET|PUT|DELETE /api/cost-management/v1/recommendations/openshift/settings/gpu
```

Container list/detail (`GET /recommendations/openshift`, `.../detail`) include the `gpu` enrichment block when the plugin is enabled.

## Notification codes

GPU-related codes include **10** (GPU underutilized), **26–28** (MIG/time-slicing), and **36**. Idle/zombie GPU workloads may also surface container idle codes **5** / **8** on the parent row.

Filter: `GET /recommendations/openshift/notification-codes?filter[plugin]=gpu`.

See [Notification codes — GPU](../architecture/notification-codes.md#gpu-containers-and-time-slicing).

## Savings

GPU savings are computed at **API read-time** per container/node detail request. They are **not persisted** to the database and are excluded from fleet `savings-summary` totals (`by_plugin.gpu` always returns 0).

To obtain GPU dollar savings, query individual container or node detail endpoints. GPU does not emit `NotifNoCostData` (code 25) — when GPU cost data is unavailable, savings fields are omitted entirely.

## Architecture

- [GPU classification](../architecture/gpu-classification.md)
- [GPU catalogs](../architecture/gpu-catalogs.md)
- [Cost integration](../architecture/cost-integration.md)
- [GPU MIG](../features/gpu-mig.md) · [GPU time-slicing](../features/gpu-time-slicing.md)
