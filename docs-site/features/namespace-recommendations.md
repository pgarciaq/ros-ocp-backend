# Namespace Recommendations

!!! info "Quick Facts"
    **API:** `GET /api/cost-management/v1/recommendations/openshift/namespaces`  
    **Plugin:** `namespace` (priority 90; stays enabled in Kruize mode for HTTP routes)  
    **Configurable:** Per-org Settings API + admin env vars  
    **Engines:** `cost` and `performance` on every response  
    **OpenShift only:** Requires namespace ROS CSV from the metrics operator

Right-size **namespace-level CPU and memory requests/limits** by aggregating
container usage digests per namespace, then applying the same percentile-based
sizing engine used for containers (with namespace-specific defaults).

**Related:** [ResourceQuota recommendations](quota-recommendations.md) tune existing
`ResourceQuota` **hard** limits against container sums. Namespace recommendations
propose ideal namespace totals from observed usage — a different feature.

Internal design: [`docs/features/namespace-recommendations.md`](../../../docs/features/namespace-recommendations.md) (repo `docs/` tree).

---

## How it works

```mermaid
flowchart TD
  Op[Metrics operator] --> CSV[Namespace ROS CSV]
  Cont[Container plugin] --> Digests[daily_container_digests]
  CSV --> ND[daily_namespace_digests]
  ND --> Eng[RecommendAllNamespaces]
  Eng --> API[GET .../namespaces/]
```

1. The operator emits `ocp_ros_namespace_usage` (or `ros-openshift-namespace-*.csv`)
   for namespaces labeled for optimization (see [Enablement](#enablement)).
2. ROS ingests namespace digests; the container plugin runs first (lower priority).
3. `RecommendAllNamespaces` sizes each namespace × term × engine using percentile
   profiles, adaptive margin, limit multiplier, and optional OOM bump.
4. Snapshots are written to `historical_namespace_recommendation_sets` for the
   history API.

---

## Dual engine and terms

| Engine | CPU percentile (defaults) | Memory percentile (defaults) |
|--------|----------------------------|------------------------------|
| `cost` | P60 | P95 |
| `performance` | P98 | Max (P100) |

| Term | Window | Min data days |
|------|--------|---------------|
| `short_term` | 1 day | 1 |
| `medium_term` | 7 days | 3 |
| `long_term` | 15 days | 7 |

List and detail responses nest engines under
`recommendations.recommendation_terms.{short|medium|long}_term.{cost|performance}`.

---

## API

```http
GET /api/cost-management/v1/recommendations/openshift/namespaces
GET /api/cost-management/v1/recommendations/openshift/namespaces/{recommendation-id}
GET /api/cost-management/v1/recommendations/openshift/namespaces/{recommendation-id}/history
```

Legacy aliases (deprecated): `GET .../openshift/namespace/recommendations`,
`GET .../namespace/{recommendation-id}`.

### List filters and sorting

| Parameter | Description |
|-----------|-------------|
| `filter[cluster]` / `cluster` | Cluster UUID |
| `filter[project]` / `project` / `namespace` | Namespace name (exact; comma-separated OR) |
| `filter[tag:<key>]` | OpenShift label filter (`ROS_TAGS_ENABLED=true`) |
| `order_by` / `order_how` | Sort (e.g. `namespace`, variation fields) |
| `limit` / `offset` | Pagination (default limit 100, max 1000) |

### History

History returns prior snapshots from `historical_namespace_recommendation_sets`.
Each DB row expands to separate **`cpu`** and **`memory`** entries.

| Parameter | Description |
|-----------|-------------|
| `filter[term]` | `short_term`, `medium_term`, `long_term` (or `short` / `medium` / `long`) |
| `filter[engine]` | `cost` or `performance` |
| `limit` | Snapshots to return (default 30, max 90) |

Each history entry includes `resource`, `term`, `recorded_at`, `recommended`,
`current`, and optional `notification_codes`.

---

## Business hours

When `ROS_BUSINESS_HOURS_ENABLED=true` and a schedule exists (cluster, namespace,
or org), the engine persists rows per `schedule_type` (`all_hours`, `business_hours`).
Detail responses include a `business_hours` block under each engine after reship
completes. See [Business Hours](business-hours.md).

---

## Notification codes

Namespace recommendations use the native notification set (not Kruize optimization
notice codes `323004`–`324004`):

| Code | Name | Typical trigger |
|------|------|-----------------|
| 1 | `NotifLowConfidence` | `confidence_level` below threshold with some data |
| 7 | `NotifNewWorkload` | Less than one day of digest data |
| 9 | `NotifMemoryTrendingUp` | Memory slope above **500 KiB/day** (namespace default) |

See [Notification codes](../architecture/notification-codes.md).

---

## Configuration

**Settings API:** `GET` / `PUT` / `DELETE`
`/api/cost-management/v1/recommendations/openshift/settings/thresholds?recommendation_type=namespace`

**Term windows:** `GET .../settings/terms?recommendation_type=namespace`

Key env vars: `ROS_NAMESPACE_CPU_COST_PERCENTILE`, `ROS_NAMESPACE_CPU_PERF_PERCENTILE`,
`ROS_NAMESPACE_MEM_*`, `ROS_NAMESPACE_CPU_FLOOR_MC`. See
[Configuration](../configuration.md) and [Configurable Thresholds](configurable-thresholds.md).

---

## Enablement

1. Add `namespace` to `ROS_ENABLED_PLUGINS` (default native engine includes it).
2. Label namespaces for ROS collection (operator ≥ 4.1.0):

   ```bash
   oc label namespace NAMESPACE cost_management_optimizations=true --overwrite
   ```

   Legacy label: `insights_cost_management_optimizations=true`.

Operator CSV fields: [Namespace metrics](https://github.com/project-koku/koku-metrics-operator/blob/main/docs/report-fields-description.md#2-namespace-metrics)
(29 columns including optional `*_namespace_used` quota-used sums).

---

## Operator data

The namespace ROS file includes workload aggregates plus optional ResourceQuota
**used** columns (`cpu_request_namespace_used`, `cpu_limit_namespace_used`,
`memory_request_namespace_used`, `memory_limit_namespace_used`) for observability.
Those columns feed digest ingestion; they do **not** replace the separate
[quota plugin](quota-recommendations.md) for ResourceQuota right-sizing.
