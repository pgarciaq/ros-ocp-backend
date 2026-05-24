# ROS UI Integration Guide

Practical API reference for **koku-ui** developers building OpenShift Resource Optimization
(ROS) pages against the native Go engine in `ros-ocp-backend`.

**Authoritative spec:** [`openapi.json`](../openapi.json) at the repository root (also served at
`GET /api/cost-management/v1/recommendations/openshift/openapi.json`).

**Related docs:**

- [Configurability Reference](architecture/configurability.md) — env vars, defaults, tuning by use case
- [Recommendation Engines](architecture/recommendation-engines.md) — algorithm behavior
- [Cost Integration](architecture/cost-integration.md) — savings formulas and currency
- [Business Hours](features-business-hours.md) — schedule design and reship flow

---

## 1. Authentication

### Identity header

All ROS endpoints require the standard Cost Management identity header:

```http
x-rh-identity: <base64-encoded JSON>
```

The middleware extracts `org_id` from the decoded identity and scopes all data to that tenant.
Missing or invalid identity returns `401`:

```json
{ "status": "error", "message": "missing or invalid identity" }
```

Development example (test customer):

```bash
IDENTITY=$(echo -n '{"identity":{"account_number":"10001","org_id":"1234567","type":"User","user":{"username":"user_dev","email":"user_dev@foo.com","is_org_admin":true,"access":{}}},"entitlements":{"cost_management":{"is_entitled":true}}}' | base64 -w0)
```

> **On-prem Keycloak:** Store bare numeric `org_id` (e.g. `"1234567"`), not `"org1234567"`.
> Koku prepends `org` to form the tenant schema.

### Base URL

| Deployment | Base path |
|------------|-----------|
| Console / SaaS | `https://<host>/api/cost-management/v1` |
| On-prem (via koku-ui proxy) | `/api/cost-management/v1` |

All paths in this guide are relative to that prefix.

Example:

```http
GET /api/cost-management/v1/recommendations/openshift
x-rh-identity: <token>
```

### RBAC

When `RBAC_ENABLED=true`, list endpoints filter by `openshift.cluster` and `openshift.project`
permissions from the identity. Users with `*` on a resource see all clusters/projects.

### Cache headers

Recommendation responses include `Cache-Control: no-store`. Do not rely on browser or CDN caching.

---

## 2. Recommendation List (Container / Namespace)

### Container recommendations

```http
GET /recommendations/openshift
```

Returns one row per **container** (paginated by distinct containers), with all term/engine
variants nested under `recommendations`.

**Detail:**

```http
GET /recommendations/openshift/{recommendation-id}
```

`recommendation-id` is a deterministic UUID v5 derived from
`cluster_uuid/namespace/workload/workload_type/container`.

### Namespace recommendations

```http
GET /recommendations/openshift/namespaces
GET /recommendations/openshift/namespaces/{recommendation-id}
```

Same nesting pattern; no `container`, `workload`, or `workload_type` fields on namespace rows.

Legacy alias (deprecated): `GET /recommendations/openshift/namespace/{id}`.

### Query parameters (container list)

| Parameter | Description |
|-----------|-------------|
| `cluster` | Cluster UUID **or** cluster alias (substring match). Use `filter[exact:cluster]` for exact alias. Not `cluster_uuid`. |
| `project` | Namespace filter (maps to `workloads.namespace` / `rs.namespace`). Supports `exclude[project]`, `filter[exact:project]`. |
| `workload` | Deployment/StatefulSet/etc. name filter. |
| `workload_type` | One of: `daemonset`, `deployment`, `deploymentconfig`, `replicaset`, `replicationcontroller`, `statefulset`. |
| `container` | Container name filter. |
| `start_date`, `end_date` | `YYYY-MM-DD` date range on `updated_at` (default: current month). |
| `stale` | Staleness filter — see [Stale flag](#stale-flag). |
| `has_gpu` | `true` / `false` — filter containers with GPU recommendations. |
| `gpu_model` | Substring match on GPU model name (repeatable). |
| `gpu_classification` | Exact match: `idle`, `underutilized`, `memory_bound`, `compute_bound_underutil`, `well_utilized`, `no_profiling`, etc. |
| `order_by` | Sort column — see [Sorting](#sorting). |
| `order_how` | `asc` or `desc` (default `desc`). |
| `offset`, `limit` | Pagination (default limit 100, max 1000). |
| `format` | `json` (default) or `csv`. |
| `cpu-unit` | `cores` (default on list) or `millicores`. |
| `memory-unit` | `bytes` (default on list), `MiB`, or `GiB`. |
| `true-units` | When `false` (default), memory/CPU use k8s-style formats in legacy JSON paths. |

> **Note:** There is **no** server-side `engine` or `recommendation_type` filter on container/namespace
> list endpoints. Both **cost** and **performance** engines are always returned nested. The UI selects
> which engine to display. CSV export expands to one row per term × engine.

Namespace list supports `cluster`, `project`, date range, `stale`, `order_by`, `order_how`,
`offset`, `limit`, and `format=csv`.

### List response envelope

```json
{
  "meta": { "count": 42, "limit": 100, "offset": 0 },
  "links": {
    "first": "...?limit=100&offset=0",
    "previous": "...",
    "next": "...",
    "last": "..."
  },
  "data": [ /* container or namespace objects */ ]
}
```

### Native list item (container)

```json
{
  "id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
  "cluster_alias": "my-cluster",
  "cluster_uuid": "02059694-68ab-4d58-8809-de1e91f1d0e5",
  "container": "app",
  "project": "team-a",
  "workload": "api",
  "workload_type": "deployment",
  "source_id": "12345",
  "last_reported": "2026-05-20T12:00:00Z",
  "replicas": { "min": 2, "max": 3, "avg": 2, "desired": 3, "available": 3, "source": "kube_state_metrics" },
  "estimated_monthly_savings_usd": 12.50,
  "currency": "USD",
  "recommendations": {
    "short_term": {
      "cost": { /* EngineRecommendation */ },
      "performance": { /* EngineRecommendation */ }
    },
    "medium_term": { "cost": {}, "performance": {} },
    "long_term": { "cost": {}, "performance": {} }
  },
  "gpu": {
    "medium_term": {
      "gpu_classification": "underutilized",
      "recommended_gpu_profile": "1g.5gb",
      "gpu_confidence": 0.8,
      "estimated_monthly_gpu_savings_usd": 45.0,
      "currency": "USD"
    }
  }
}
```

`gpu` is present only when the GPU plugin is enabled and the workload uses GPUs.

### Detail response (Kruize-compatible shape)

Detail endpoints transform flat native fields into the nested structure the existing UI expects:

```json
{
  "recommendations": {
    "current": {
      "requests": { "cpu": { "amount": 0.5, "format": "cores" }, "memory": { "amount": 512, "format": "MiB" } },
      "limits": { }
    },
    "monitoring_end_time": "2026-05-20T12:00:00Z",
    "estimated_monthly_savings_usd": 12.50,
    "recommendation_terms": {
      "medium_term": {
        "duration_in_hours": 168,
        "plots": { "plots_data": { /* boxplot quartiles */ } },
        "recommendation_engines": {
          "cost": {
            "config": { "requests": { }, "limits": { } },
            "variation": { "requests": { "cpu": { "amount": -15, "format": "percentage" } } },
            "notifications": { "1": { "type": "WARNING", "message": "...", "code": 1 } },
            "business_hours": {
              "requests": { "cpu": { "amount": 0.8, "format": "cores" } },
              "limits": { }
            }
          },
          "performance": { }
        }
      }
    }
  }
}
```

### All-hours vs business-hours

| Perspective | Location | When present |
|-------------|----------|--------------|
| **All hours** (default) | `recommendation_engines.{cost\|performance}.config` | Always |
| **Business hours** | `recommendation_engines.{cost\|performance}.business_hours` | Schedule configured **and** enabled for the workload's namespace; reship complete |

- `business_hours` mirrors the `config` shape (`requests` / `limits` with `amount` + `format`).
- When business-hours data is unavailable, the field is **omitted** (not `null`).
- `business_hours.reason` may explain degraded mode (e.g. reship in progress).

Show both perspectives side-by-side when `business_hours` is present. Default table columns to
**all-hours / cost / medium_term** unless the user selects otherwise.

### Cost vs performance engines

| Engine | Intent | Typical sizing |
|--------|--------|----------------|
| `cost` | Minimize spend | Lower CPU/memory percentiles (e.g. P60 CPU, P95 memory) |
| `performance` | Minimize throttling/OOM risk | Higher percentiles (e.g. P98 CPU, max memory) |

**UI default:** Show **cost** engine first; offer **performance** as a tab or toggle.

Each engine object includes:

| Field | Description |
|-------|-------------|
| `cpu_request_millicores`, `cpu_limit_millicores` | Recommended CPU (list view) |
| `memory_request_kib`, `memory_limit_kib` | Recommended memory (list view) |
| `current_*` | Current requests/limits |
| `variation_*_pct` | Percent change vs current (negative = downsizing) |
| `confidence_level` | 0.0–1.0 — see [Confidence](#confidence-score) |
| `notifications` | Map keyed by code string — see [Notifications](#notifications) |
| `notification_codes` | Raw int16 array (list view) |

### Savings fields

| Field | Scope | Notes |
|-------|-------|-------|
| `estimated_monthly_savings_usd` | Container/namespace row | Based on **cost** engine, **medium** term by default in aggregation |
| `currency` | Row or cluster | ISO currency from Koku cost model (default `USD`) |
| GPU: `estimated_monthly_gpu_savings_usd` | `gpu.{term}` | MIG/profile savings |
| GPU: `estimated_monthly_timeslicing_savings_usd` | `gpu.{term}` | Per-container time-slicing savings |

When Koku has no cost model rates, notification code **25** (`no cost data`) is emitted and
savings fields may be absent.

**Negative savings:** The workload needs **more** resources than currently requested. Display as
“needs additional resources” rather than a negative dollar amount.

### Notifications

Notifications use a Kruize-compatible map:

```json
"notifications": {
  "5": {
    "type": "INFO",
    "message": "Workload uses less than 1% of requested resources",
    "code": 5
  }
}
```

See [Section 12 — Notification codes](#notification-codes-reference) for the full table.

### Confidence score

`confidence_level` = `min(days_of_data / window_days, 1.0)` for the term's observation window.

| Range | UI treatment |
|-------|--------------|
| ≥ 0.5 | Normal display |
| < 0.5 | Warning badge; notification code **1** may also appear |
| GPU workloads | Additional tiering by profiling days; may be reduced for bursty or no-profiling cases |

### Stale flag

A recommendation is **stale** when the cluster stopped sending metrics beyond
`ROS_STALENESS_THRESHOLD_HOURS` (default 72h). Stale rows get notification code **2**.

| `?stale=` | Behavior |
|-----------|----------|
| omitted or `false` | **Exclude** stale (default) |
| `true` | Include stale **and** fresh |
| `only` | **Only** stale rows |

Detail lookups exclude stale rows (`stale = false` in DB query).

### Idle / abandoned classification

Detected via notification codes (not a separate column):

| Code | Classification | Savings behavior |
|------|----------------|------------------|
| **5** | Idle — usage below CPU/memory idle thresholds but not all zero | 100% of current request cost recoverable |
| **8** | Abandoned — zero CPU **and** zero memory across the window | 100% recoverable; supersedes idle |

Highlight these rows for potential decommissioning. Show full savings estimate when present.

---

## 3. Node Recommendations

### Node CPU/memory utilization

```http
GET /recommendations/openshift/nodes
```

Deprecated alias: `GET /recommendations/openshift/nodes/utilization` (returns `Deprecation: true` header).

#### Query parameters

| Parameter | Description |
|-----------|-------------|
| `cluster_uuid` | Filter by cluster UUID |
| `node` | Filter by node name |
| `term` | `short`, `medium`, or `long` — filters which term rows contribute (response still nests all returned terms) |
| `engine` | `cost` or `performance` — filters engine rows |
| `is_underutilized` | `true` / `false` |
| `is_overcommitted` | `true` / `false` |
| `order_by` | `node` or `estimated_monthly_savings_usd` (default) |
| `order_how` | `asc` or `desc` (default `desc`) |
| `offset`, `limit` | Pagination (default limit 10, max 1000) |

#### Response structure

One object per node with nested terms and engines:

```json
{
  "meta": { "count": 5, "limit": 10, "offset": 0, "currency": "USD" },
  "data": [
    {
      "node": "worker-1",
      "cluster_uuid": "...",
      "recommendation_type": "cpu_memory_utilization",
      "classification": {
        "is_underutilized": true,
        "is_overcommitted": false,
        "stranded_resource": "memory"
      },
      "metrics": {
        "cpu_util_p50": 0.12,
        "cpu_util_p95": 0.28,
        "mem_util_p50": 0.45,
        "mem_util_p95": 0.62
      },
      "pod_count": 42,
      "cpu_overcommit_ratio": 1.1,
      "trend_slope": 0.02,
      "recommendation_terms": {
        "medium_term": {
          "recommendation_engines": {
            "cost": {
              "recommended_cpu_cores": 8.0,
              "recommended_memory_gib": 64.0,
              "node_count_reduction": 1,
              "estimated_monthly_savings_usd": 500.0,
              "notifications": { },
              "updated_at": "2026-05-20T10:00:00Z"
            },
            "performance": { }
          }
        }
      }
    }
  ],
  "links": { },
  "warnings": []
}
```

#### Classification types

| Signal | Meaning |
|--------|---------|
| `is_underutilized: true` | CPU P95 **and** memory P95 below underutil threshold (default 30% of allocatable) |
| `is_overcommitted: true` | Sum of pod CPU requests exceeds overcommit threshold × allocatable (default 1.5×) |
| `stranded_resource` | `"cpu"` or `"memory"` when CPU/memory utilization is imbalanced (stranded capacity) |
| Neither flag | Effectively **well_utilized** for display purposes |

Notification codes **11** (underutilized), **12** (overcommitted), **13** (stranded) align with these flags.

#### Savings fields

`estimated_monthly_savings_usd` on each engine reflects consolidation / right-sizing opportunity
for that engine profile. `node_count_reduction` suggests how many nodes could be removed (cost engine).

### GPU time-slicing (separate endpoint)

Node-level GPU time-slicing is **not** under `/nodes`:

```http
GET /recommendations/openshift/gpu/timeslicing
```

| Parameter | Description |
|-----------|-------------|
| `node_name` | Filter by node |
| `gpu_model` | Filter by GPU model |
| `term` | Term filter |
| `order_by` | `node_name`, `cluster_uuid`, `gpu_model`, `recommended_replicas`, `confidence`, `total_node_savings_usd` |
| `offset`, `limit` | Pagination |

```json
{
  "meta": { "count": 2, "limit": 20, "offset": 0, "total_savings_usd": 1200.0, "currency": "USD" },
  "data": [
    {
      "node_name": "gpu-node-1",
      "cluster_uuid": "...",
      "term": "medium",
      "recommendation_type": "gpu_timeslicing",
      "gpu_model": "NVIDIA-A100",
      "recommended_replicas": 4,
      "savings_per_gpu_usd": 150.0,
      "total_node_savings_usd": 450.0,
      "confidence": 0.65,
      "candidate_containers": [ { "namespace": "ml", "workload": "train", "container": "worker", "classification": "underutilized" } ],
      "impacted_containers": [ ],
      "notification_codes": [36]
    }
  ]
}
```

Link from container GPU data: `time_slicing_node` and `time_slicing_replicas` on container `gpu` objects.

---

## 4. PVC Recommendations

```http
GET /recommendations/openshift/pvcs
```

### Query parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `cluster_uuid` | — | Filter by cluster |
| `namespace` | — | Filter by namespace |
| `recommendation_type` | — | `oversized`, `near_full`, `orphaned`, `healthy` |
| `term` | `medium` | `short`, `medium`, or `long` |
| `offset`, `limit` | 0, 20 | Max limit 100 |

### Response structure

```json
{
  "meta": { "count": 10, "limit": 20, "offset": 0, "currency": "USD" },
  "links": { },
  "data": [
    {
      "cluster_uuid": "...",
      "namespace": "team-a",
      "persistentvolumeclaim": "data-pvc",
      "persistentvolume": "pv-abc",
      "storageclass": "gp3",
      "capacity_bytes": 107374182400,
      "usage_bytes_max": 10737418240,
      "usage_ratio": 0.10,
      "recommendation_type": "oversized",
      "recommended_bytes": 21474836480,
      "days_to_full": null,
      "growth_bytes_per_day": 1048576,
      "estimated_monthly_savings_usd": 8.50,
      "notifications": { "29": { "type": "INFO", "message": "...", "code": 29 } },
      "data_days": 14,
      "term": "medium",
      "resize_note": "Kubernetes does not support in-place PVC shrinking..."
    }
  ]
}
```

### Classification types

| Type | Condition (defaults) | UI emphasis |
|------|----------------------|-------------|
| `oversized` | Usage/capacity < 20% for 3+ days | Show `recommended_bytes`; display `resize_note` |
| `near_full` | Usage/capacity > 85% **or** projected full within 30 days | Urgent badge |
| `orphaned` | Zero usage for 3+ days | Deletion candidate; show `resize_note` |
| `healthy` | Between thresholds | Informational only; typically omit from optimization views |

### Savings fields

`estimated_monthly_savings_usd` = savings from reducing provisioned capacity to `recommended_bytes`
(using Koku storage rates or fallback).

### Growth trend and days-to-full

| Field | Description |
|-------|-------------|
| `growth_bytes_per_day` | Linear regression slope on daily average usage |
| `days_to_full` | Projected days until capacity exhausted at current growth rate; `null` if not applicable |

Requires minimum trend data (default 7 days). Near-full alerts can fire on projection even when
current usage is below 85%.

---

## 5. Snapshot Recommendations

```http
GET /recommendations/openshift/snapshots
```

### Query parameters

| Parameter | Description |
|-----------|-------------|
| `cluster_uuid` | Filter by cluster |
| `namespace` | Filter by namespace |
| `recommendation_type` | Classification filter — see below |
| `offset`, `limit` | Pagination (default limit 20, max 100) |

### Response structure

```json
{
  "meta": { "count": 3, "limit": 20, "offset": 0, "currency": "USD" },
  "data": [
    {
      "cluster_uuid": "...",
      "namespace": "team-a",
      "snapshot_name": "snap-data-20260101",
      "source_pvc_name": "data-pvc",
      "volume_snapshot_class": "csi-snapclass",
      "storageclass": "gp3",
      "creation_timestamp": "2026-01-01T00:00:00Z",
      "restore_size_bytes": 10737418240,
      "age_days": 120,
      "source_pvc_exists": false,
      "restored_pvc_count": 0,
      "managed_by": "velero",
      "recommendation_type": "orphaned",
      "estimated_monthly_cost_usd": 0.52,
      "notifications": { "31": { "type": "WARNING", "message": "...", "code": 31 } }
    }
  ]
}
```

### Classification types

| Type | Meaning |
|------|---------|
| `orphaned` | Source PVC deleted and age > orphan threshold (default 7 days) |
| `never_restored` | Zero restores and age > never-restored threshold (default 30 days) |
| `stale` | Age > stale threshold (default 90 days), never restored, not managed |
| `redundant` | More than N snapshots per PVC; this one is outside the N most recent |
| `managed` | Backup-tool labels detected (Velero, OADP, etc.) — review retention, not auto-delete |
| `active` | Recent or has restore history — no action |

### Cost estimation fields

`estimated_monthly_cost_usd` = `restore_size_bytes` × storage rate (from Koku effective rates,
tenant override, or default $0.05/GiB/month). This is **ongoing cost**, not savings — sum for
waste dashboards.

---

## 6. Savings Summary

```http
GET /recommendations/openshift/savings-summary
```

Fleet-wide aggregated savings for dashboard hero metrics.

### Query parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `engine` | `cost` | `cost` or `performance` |

### Response structure

```json
{
  "currency": "USD",
  "total_estimated_monthly_savings_usd": 12500.75,
  "by_cluster": [
    {
      "cluster_uuid": "...",
      "cluster_alias": "prod-east",
      "savings": 8200.50,
      "has_cost_data": true
    }
  ],
  "by_plugin": {
    "container": 5000.0,
    "gpu": 0.0,
    "node": 3000.0,
    "pvc": 1500.0,
    "snapshot": 500.0
  },
  "gpu_savings_note": "GPU savings are computed at API read time and are not included in this fleet summary..."
}
```

### What `has_cost_data` means

Per cluster in `by_cluster`:

- `true` — At least one recommendation exists **and** not all of them have notification code **25** (no cost data).
- `false` — All recommendations lack cost model rates, or no cost-bearing recs exist.

When `has_cost_data` is false, show “cost model not configured” instead of `$0.00`.

**Note:** `by_plugin.gpu` is always `0` in this summary; use GPU-specific endpoints for GPU dollar estimates.
`snapshot` sums **cost** (waste), not savings.

Related: `GET /recommendations/openshift/fleet-summary` for counts (idle/abandoned/stale containers).

---

## 7. Settings: Terms

Control observation windows per recommendation plugin.

```http
GET    /recommendations/openshift/settings/terms?recommendation_type={plugin}
PUT    /recommendations/openshift/settings/terms?recommendation_type={plugin}
DELETE /recommendations/openshift/settings/terms?recommendation_type={plugin}
```

### Valid `recommendation_type` values

Plugins implementing `TermProvider`: `container`, `namespace`, `node`, `gpu`, `pvc`.

### What terms control

| Field | Purpose |
|-------|---------|
| `window_days` | Observation window length |
| `min_data_days` | Minimum days of data required before emitting recommendations |
| `decay_halflife_hours` | Exponential decay for weighting recent data (0 = no decay) |

Three named terms: `short`, `medium`, `long` (1–3 items on PUT).

### GET response

```json
{
  "recommendation_type": "container",
  "terms": [
    {
      "name": "short",
      "window_days": 1,
      "min_data_days": 1,
      "decay_halflife_hours": 0,
      "locked": false,
      "is_default": true
    },
    {
      "name": "medium",
      "window_days": 7,
      "min_data_days": 3,
      "decay_halflife_hours": 168,
      "locked": false,
      "is_default": true
    }
  ]
}
```

### Locked fields

When an admin sets `ROS_TERMS_{PLUGIN}_{TERM}_{FIELD}` env vars, the corresponding term shows
`locked: true`. PUT returns `422` with `locked_terms` if a locked term is modified.

### Effect on recommendations

Changes apply on the **next ingestion/recommendation cycle** (not retroactive to cached API responses).
Call `DELETE` to reset to compiled defaults.

Default max windows: 90 days (container/namespace/node/gpu), 365 days (pvc).

---

## 8. Settings: Thresholds

Per-tenant sizing and classification thresholds (native engine only).

```http
GET    /recommendations/openshift/settings/thresholds?recommendation_type={type}
PUT    /recommendations/openshift/settings/thresholds?recommendation_type={type}
DELETE /recommendations/openshift/settings/thresholds?recommendation_type={type}
```

`recommendation_type`: `container`, `namespace`, `node`, `gpu`, `pvc`.

GET returns merged settings plus `locked_fields` (admin env overrides).
PUT accepts partial JSON; validates ranges before save. On success, ROS triggers **async
recalculation** for all clusters in the org using existing digest data—recommendations
reflect the new thresholds within seconds without waiting for the next ingestion cycle.
DELETE resets tenant overrides to defaults.
Locked PUT attempts return `403` with `locked_fields`.

### Container / namespace fields

| Field | Default | Validation | Description |
|-------|---------|------------|-------------|
| `cpu_cost_percentile` | 0.60 | 0.01–1.0 | Cost engine CPU percentile |
| `cpu_perf_percentile` | 0.98 | 0.01–1.0 | Performance engine CPU percentile |
| `mem_cost_percentile` | 0.95 | 0.01–1.0 | Cost engine memory percentile |
| `mem_perf_percentile` | 1.0 | 0.01–1.0 | Performance engine memory percentile |
| `min_margin` | 1.15 | 1.0–3.0 | Adaptive margin floor |
| `max_margin` | 1.50 | 1.0–3.0 | Adaptive margin ceiling (must be ≥ min_margin) |
| `limit_multiplier` | 1.05 | 1.0–5.0 | limit = request × multiplier |
| `cpu_floor_mc` | 25 | 1–1000 | Minimum CPU request (millicores) |
| `idle_cpu_threshold_mc` | 10 | 1–10000 | Max CPU for idle detection |
| `idle_mem_threshold_kib` | 10240 | 1–10485760 | Max memory for idle detection |
| `mem_trend_slope_threshold` | 100 (container), 500 (namespace) | 1–1000000 | KiB/day trend alert |
| `low_confidence_threshold` | 0.5 | 0.01–1.0 | Below → low-confidence notification |

Namespace uses the same field set with a higher default `mem_trend_slope_threshold`.

### Node fields

| Field | Default | Validation |
|-------|---------|------------|
| `underutil_threshold` | 0.30 | 0.01–0.99 |
| `overcommit_threshold` | 1.50 | 1.0–10.0 |
| `allocatable_factor` | 0.93 | 0.5–1.0 |
| `stranded_imbalance_threshold` | 0.60 | 0.1–1.0 |
| `ema_alpha` | 0.30 | 0.01–1.0 |
| `cost_target_utilization` | 0.80 | 0.1–0.99 |
| `perf_target_utilization` | 0.55 | 0.1–0.99 |
| `perf_consolidation_headroom_multiplier` | 2.0 | 1.0–10.0 |
| `trend_min_days` | 3 | 1–30 |

### PVC fields

| Field | Default | Validation |
|-------|---------|------------|
| `oversized_threshold` | 0.20 | 0.01–0.99 (must be < near_full) |
| `near_full_threshold` | 0.85 | 0.01–0.99 |
| `min_trend_days` | 2 | 1–365 |
| `recommended_size_multiplier` | 2 | 1–10 |
| `min_recommended_gib` | 1 | 1–10240 |
| `days_to_full_alert` | 30 | 1–365 |

### GPU fields ⚠️

> **Expert-only:** GPU thresholds interact with NVIDIA DCGM profiling semantics, MIG hardware
> sizing, and time-slicing heuristics. Incorrect values produce misleading recommendations.
> Show a warning tooltip and link to [GPU Classification](architecture/gpu-classification.md).

| Field | Default | Validation |
|-------|---------|------------|
| `idle_threshold` | 0.02 | 0.0–1.0 |
| `underutilized_sm_threshold` | 0.25 | 0.0–1.0 |
| `underutilized_tensor_threshold` | 0.15 | 0.0–1.0 |
| `membound_dram_threshold` | 0.60 | 0.0–1.0 |
| `membound_tensor_threshold` | 0.15 | 0.0–1.0 |
| `fb_headroom_factor` | 1.20 | 0.0–1.0 |
| `compute_bound_dram_threshold` | 0.30 | 0.0–1.0 |
| `mig_fb_percentile` | 0.98 | 0.0–1.0 |
| `confidence_days_tier1` | 3 | 1–365 (must be < tier2) |
| `confidence_days_tier2` | 7 | 1–365 (must be < tier3) |
| `confidence_days_tier3` | 14 | 1–365 |
| `spike_ratio_threshold` | 5.0 | 1.0–100.0 |
| `spike_confidence_penalty` | 0.70 | 0.01–1.0 |
| `no_profiling_confidence_factor` | 0.50 | 0.01–1.0 |
| `timeslicing_majority_threshold` | 0.50 | 0.01–1.0 |
| `timeslicing_min_replicas` | 2 | 1–16 (must be ≤ max) |
| `timeslicing_max_replicas` | 8 | 1–16 |
| `timeslicing_base_penalty` | 0.70 | 0.01–1.0 |
| `timeslicing_impacted_weight` | 0.30 | 0.01–1.0 |
| `node_freshness_days` | 7 | 1–90 |

### Recommended values by use case

See [Configurability — Recommended Values by Use Case](architecture/configurability.md#recommended-values-by-use-case)
for aggressive cost optimization, stability-first, GPU training, and batch storage profiles.

---

## 9. Settings: Snapshot

```http
GET /recommendations/openshift/settings/snapshot
PUT /recommendations/openshift/settings/snapshot
```

No DELETE — PUT partial fields to override; omit locked fields.

| Field | Default | Description |
|-------|---------|-------------|
| `orphan_age_days` | 7 | Days after source PVC deletion before orphaned classification |
| `never_restored_days` | 30 | Days without any restore before never-restored classification |
| `stale_days` | 90 | Age threshold for stale classification |
| `redundant_threshold` | 3 | Max snapshots per PVC before older ones flagged redundant |
| `cost_per_gib_month_usd` | 0.05 | Fallback $/GiB/month when Koku rates unavailable |
| `locked_fields` | [] | Present on GET — fields controlled by admin env vars |

---

## 10. Settings: Business Hours

Requires `ROS_BUSINESS_HOURS_ENABLED=true` and `business_hours: true` from capabilities.

### Org default

```http
GET    /recommendations/openshift/settings/business-hours
PUT    /recommendations/openshift/settings/business-hours
DELETE /recommendations/openshift/settings/business-hours
```

### Cluster override

```http
GET    /recommendations/openshift/settings/business-hours/clusters/{cluster_uuid}
PUT    /recommendations/openshift/settings/business-hours/clusters/{cluster_uuid}
DELETE /recommendations/openshift/settings/business-hours/clusters/{cluster_uuid}
```

### Namespace override

```http
GET    /recommendations/openshift/settings/business-hours/clusters/{cluster_uuid}/namespaces/{namespace}
PUT    /recommendations/openshift/settings/business-hours/clusters/{cluster_uuid}/namespaces/{namespace}
DELETE /recommendations/openshift/settings/business-hours/clusters/{cluster_uuid}/namespaces/{namespace}
```

Resolution order: **namespace → cluster → org default**.

### Schedule format (PUT body)

```json
{
  "timezone": "America/New_York",
  "enabled": true,
  "off_hours_weight": 0.1,
  "schedule": {
    "days": ["monday", "tuesday", "wednesday", "thursday", "friday"],
    "start_time": "08:00",
    "end_time": "17:00"
  }
}
```

| Field | Rules |
|-------|-------|
| `timezone` | IANA timezone name |
| `days[]` | Lowercase English day names |
| `start_time`, `end_time` | `HH:MM` 24-hour format |
| `off_hours_weight` | 0.0–1.0 — weight for samples outside the window (default 0.1) |
| `enabled` | `false` disables business-hours digests for the scope |

PUT returns `warnings` including storage-doubling notice when enabling.

### Reship status (cluster GET only)

Cluster and namespace GET responses include:

| Field | Values |
|-------|--------|
| `reship_status` | `complete`, `pending`, `forward_only` |
| `reship_status_since` | ISO timestamp when current status began |

| Status | UI treatment |
|--------|--------------|
| `complete` | Business-hours recommendations trustworthy |
| `pending` | Show “Recalculating business-hours data…” banner; `business_hours` may be absent |
| `forward_only` | **Degraded mode** — historical backfill unavailable; only forward data used. Show persistent warning banner explaining recommendations may not reflect full history |

Org-level GET does **not** include `reship_status`.

After schedule changes, expect one ingestion cycle before updated `business_hours` blocks appear in recommendations.

---

## 11. Settings: Capabilities

```http
GET /recommendations/openshift/settings/capabilities
```

Discover which plugins and features are active for conditional UI rendering.

```json
{
  "recommendation_types": [
    { "name": "container", "supports_terms": true, "enabled": true },
    { "name": "gpu", "supports_terms": true, "enabled": true },
    { "name": "node", "supports_terms": true, "enabled": true },
    { "name": "pvc", "supports_terms": true, "enabled": true },
    { "name": "namespace", "supports_terms": true, "enabled": true },
    { "name": "snapshot", "supports_terms": false, "enabled": true }
  ],
  "business_hours": true
}
```

| Field | Use in UI |
|-------|-----------|
| `enabled: false` | Hide nav items and API calls for that domain (endpoint returns 404 when plugin disabled) |
| `supports_terms` | Show term configuration in Settings when true |
| `business_hours` | Show business-hours settings and dual-perspective recommendation UI |

Legacy Kruize mode excludes disabled Kruize plugin from the list.

---

## 12. Common Patterns

### Pagination

Standard `offset` + `limit` with `meta.count` and `links.first|previous|next|last`.

Container/namespace lists paginate by **distinct containers/namespaces**, not by raw DB rows
(each container row includes all term × engine combinations).

### Sorting

ROS uses **`order_by` + `order_how`**, not a `-` prefix on column names.

Common container `order_by` values:

- `cluster`, `project`, `workload`, `workload_type`, `container`, `last_reported`
- `cpu_request_current`, `memory_request_current`
- `cpu_variation_medium_cost`, `cpu_variation_medium_performance`, etc.

Default sort: `last_reported` descending.

### Currency

Always read the `currency` field from the response (`USD`, `EUR`, etc.). Never hardcode `$`.
Format with the user's locale; prefix/suffix based on currency code.

When `currency` is absent, fall back to `USD` (server default).

### Engine parameter

| Endpoint | `engine` support |
|----------|------------------|
| `/savings-summary` | Yes — selects cost vs performance aggregation |
| `/nodes` | Yes — filters nested engine rows |
| Container/namespace list | No — client selects from nested `cost` / `performance` |

### Notification codes reference

| Code | Severity | Message (summary) | Suggested UI treatment |
|------|----------|-------------------|------------------------|
| 1 | WARNING | Less than 4 days of data | Low-confidence badge |
| 2 | WARNING | No metrics for 48+ hours | Stale / outdated indicator |
| 3 | CRITICAL | OOM kills detected | Error badge; suggest memory increase |
| 4 | WARNING | PDB affects MachineSet scaling | Info banner before node changes |
| 5 | INFO | Uses < 1% of requests | Idle badge; decommission candidate |
| 6 | INFO | Change matches prior recommendation | Subtle “already applied?” hint |
| 7 | INFO | < 24 hours of data | Unstable recommendation warning |
| 8 | WARNING | Zero usage 72+ hours | Abandoned badge; strong decommission cue |
| 9 | WARNING | Memory trend — capacity risk | Trend chart callout |
| 10 | INFO | GPU underutilized — consider MIG | Link to GPU/MIG views |
| 11 | INFO | Node underutilized | Node consolidation hint |
| 12 | WARNING | Node overcommitted | Risk badge on node row |
| 13 | INFO | CPU/memory imbalance | Stranded resource tooltip |
| 14 | WARNING | HPA at maxReplicas | Scaling bottleneck warning |
| 15 | INFO | HPA at minReplicas sustained | Scale-down opportunity |
| 16 | WARNING | Frequent scale events | Flapping autoscaler warning |
| 17 | INFO | Variable load, no autoscaler | Configuration suggestion |
| 18 | WARNING | VM near-zero utilization | VM idle badge |
| 19 | INFO | VM oversized vs usage | VM resize hint |
| 20 | WARNING | PVC zero usage | Orphaned storage badge |
| 21 | WARNING | HPA maxReplicas sustained | HPA bottleneck (duplicate context) |
| 22 | INFO | HPA-managed — replica recs suppressed | Explain missing replica advice |
| 23 | INFO | Instance type not in catalog | No resize available |
| 24 | INFO | Deprecated instance type | Migration suggestion |
| 25 | INFO | No cost data — no savings | Hide dollar amounts; link to cost model setup |
| 26 | INFO | GPU idle — remove GPU request | GPU deallocation hint |
| 27 | INFO | GPU memory-bound — more HBM | MIG profile suggestion |
| 28 | INFO | No GPU profiling data | Reduced-confidence GPU badge |
| 29 | INFO | PVC oversized | Rightsizing opportunity |
| 30 | WARNING | PVC near full | Urgent expansion warning |
| 31 | WARNING | Snapshot source PVC deleted | Orphan snapshot badge |
| 32 | INFO | Snapshot never restored | Cleanup candidate |
| 33 | INFO | Newer snapshot exists | Redundant snapshot hint |
| 34 | INFO | Snapshot older than retention | Stale snapshot badge |
| 35 | INFO | Backup-tool managed snapshot | Caution — review retention policy |
| 36 | INFO | GPU time-slicing candidate | Link to time-slicing view |

Reference endpoint (if enabled): `GET /recommendations/openshift/notification-codes`.

Severity mapping for badges: `CRITICAL` → error, `WARNING` → warning, `INFO` → info.

---

## 13. UX Recommendations

1. **Default view:** Cost engine, medium term, all-hours perspective.
2. **Engine toggle:** Tab or segmented control for cost vs performance — do not fetch separately.
3. **Negative savings:** Phrase as “requires additional resources” with absolute usage delta, not negative currency.
4. **Low confidence:** Badge when `confidence_level` < 0.5 or codes 1/7 present.
5. **Stale:** Badge when code 2 present or row excluded by default stale filter; offer “show stale” toggle wired to `?stale=only`.
6. **Idle/abandoned:** Distinct badges (codes 5 vs 8); prioritize in “optimization opportunities” lists.
7. **Business hours:** When `business_hours` block exists, show side-by-side comparison with all-hours config; respect `reship_status` banners on cluster settings.
8. **GPU thresholds settings:** Expert-only section with warning tooltip; hide behind “Advanced” accordion.
9. **PVC oversized:** Always show `resize_note` — Kubernetes cannot shrink PVCs in place.
10. **Capabilities gate:** Call `/settings/capabilities` once per session to hide disabled plugin nav.
11. **Plugin disabled:** Disabled plugins return `404` — do not render empty states that imply zero recommendations.
12. **Currency / no cost data:** When code 25 present, show “—” instead of `$0.00` and link to cost model configuration.

---

## Appendix: Additional native endpoints

| Endpoint | Purpose |
|----------|---------|
| `GET /recommendations/openshift/gpu` | GPU summary counts + links to timeslicing/MIG lists |
| `GET /recommendations/openshift/gpu/mig` | Container MIG profile recommendations |
| `GET /recommendations/openshift/fleet-summary` | Org-wide container counts (idle/abandoned/stale) + savings |
| `GET /recommendations/openshift/history` | Historical recommendation trends |
| `GET /recommendations/openshift/quality` | OOM rate, stability, adoption metrics |
| `GET /recommendations/openshift/notification-codes` | Machine-readable notification catalog |

These endpoints follow the same authentication and pagination patterns described above.
