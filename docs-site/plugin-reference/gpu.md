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
| Retention tables | `gpu_container_digests`, `node_gpu_timeslicing_recommendations`, `node_gpu_timeslicing_recommendation_history` |

## Traits

| Trait | Supported |
|-------|-----------|
| CSVIngestor | No |
| IngestHook | Yes — after `container` CSV; upserts `gpu_container_digests` |
| APIEnricher | Yes — decorates container list/detail `gpu` map |
| APIProvider | Yes — fleet summary, time-slicing, MIG list, history |
| RetentionProvider | Yes — sweeps `gpu_container_digests`, `node_gpu_timeslicing_recommendations`, `node_gpu_timeslicing_recommendation_history` |
| TermProvider | Yes — short/medium/long (max 90 days) |

## Classification logic

Workloads are classified with a **multi-metric decision tree** (SM, tensor, DRAM)
—not a single utilization threshold. Each class maps to a distinct action (deallocate,
MIG partition, time-slicing, or no change). See [GPU Classification](../features/gpu-classification.md)
for workload examples and [GPU Classification — Architecture](../architecture/gpu-classification.md)
for thresholds and implementation.

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

## Confidence

GPU recommendations use a **different** confidence model than container/PVC/node plugins because DCGM profiling quality matters as much as day count.

| Endpoint | Field | Formula |
|----------|-------|---------|
| `GET .../gpu/mig` | `confidence` / `confidence_level` | Tiered by observation days (defaults: &lt;3 → 0.3, &lt;7 → 0.6, &lt;14 → 0.8, else 1.0), multiplied by burst penalty when `max(SM) / avg(SM)` exceeds threshold; reduced when no profiling data |
| `GET .../gpu/timeslicing` | `confidence` / `confidence_level` | Derived from average candidate-container confidence, penalized by impacted-workload ratio |

Both fields carry the same numeric value; `confidence_level` matches the standard name used by container, PVC, and node plugins.

Container list/detail `gpu.gpu_confidence` uses the same MIG engine score.

Configure tier thresholds via `GET/PUT .../settings/gpu` (`confidence_days_tier1/2/3`).

## MIG support

For MIG-capable GPUs, the engine maps P98 framebuffer usage (with headroom) to standard profiles (`1g.5gb` through `7g.40gb`, or `full_gpu`). Workloads that are not MIG candidates remain on full-GPU recommendations.

Feature doc: [GPU MIG recommendations](../features/gpu-mig.md). Catalogs: [GPU catalogs](../architecture/gpu-catalogs.md).

## Endpoints

```
GET /api/cost-management/v1/recommendations/openshift/gpu
GET /api/cost-management/v1/recommendations/openshift/gpu/mig
GET /api/cost-management/v1/recommendations/openshift/gpu/timeslicing
GET /api/cost-management/v1/recommendations/openshift/gpu/timeslicing/history
GET|PUT|DELETE /api/cost-management/v1/recommendations/openshift/settings/gpu
```

Container list/detail (`GET /recommendations/openshift`, `.../detail`) include the `gpu` enrichment block when the plugin is enabled.

## Notification codes

GPU-related codes include **10** (GPU underutilized), **26** (GPU idle), **27** (GPU memory-bound), **28** (no profiling data), and **36** (time-slicing candidate). Idle/zombie GPU workloads may also surface container idle codes **5** / **8** on the parent row.

Filter: `GET /recommendations/openshift/notification-codes?filter[plugin]=gpu`.

See [Notification codes — GPU](../architecture/notification-codes.md#gpu-containers-and-time-slicing).

## Savings

### MIG and idle (persisted)

MIG right-sizing and idle-GPU deallocation savings are persisted at ingestion in
`estimated_gpu_savings_cents` on `recommendation_sets` (migration **000136**). They refresh
when `container` is included in `POST /internal/recalculate-savings` after a Koku cost
model update.

API list/detail exposes them as `estimated_monthly_gpu_savings` (`MoneyAmount`) on the
container `gpu` block. [`enrichWithGPU()`](../../internal/api/gpu_enrichment.go) reads
persisted cents when available; otherwise computes at read time.

### Time-slicing (persisted at ingest)

Node-level time-slicing recommendations are **persisted during ingest** in
`node_gpu_timeslicing_recommendations` (live) and
`node_gpu_timeslicing_recommendation_history` (append-only). Dollar estimates are stored as
`estimated_savings_cents` / `savings_per_gpu_cents` when GPU cost-model rates are available at
ingest; otherwise those columns are NULL.

The list endpoint (`GET .../gpu/timeslicing`) reads the live table. Container enrichment reads
`time_slicing_node` and `time_slicing_replicas` from `recommendation_sets` when populated, with
a compute-at-read fallback until backfill completes.

**Backfill:** `POST /api/cost-management/v1/internal/backfill-gpu-timeslicing` (service-account auth).

See [GPU time-slicing](../features/gpu-time-slicing.md).

### Fleet rollup

All GPU dollar estimates are **excluded** from `GET .../savings-summary` totals
(`by_plugin.gpu` always returns `0`; see `gpu_savings_note`). Query container `gpu` blocks
or the time-slicing endpoint for GPU dollar amounts.

GPU does not emit `NotifNoCostData` (code 25) — when GPU cost data is unavailable,
savings fields are omitted entirely.

See [Savings estimations](../features/savings-estimations.md) and
[Cost integration](../architecture/cost-integration.md).

## Architecture

- [GPU classification](../architecture/gpu-classification.md)
- [GPU catalogs](../architecture/gpu-catalogs.md)
- [Cost integration](../architecture/cost-integration.md)
- [GPU MIG](../features/gpu-mig.md) · [GPU time-slicing](../features/gpu-time-slicing.md)
