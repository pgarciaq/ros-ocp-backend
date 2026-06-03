# cluster-quota

Package: [`internal/plugins/cluster-quota`](../../internal/plugins/cluster-quota/)

OpenShift **ClusterResourceQuota** right-sizing recommendations. Compares CRQ hard/used
metrics from `openshift_clusterresourcequota_usage` against aggregated namespace quota
recommendation totals.

## Plugin metadata

| Property | Value |
|----------|-------|
| Name | `cluster-quota` |
| Phase | 1 (Produce) |
| Priority | 36 (after `quota` at 35) |
| CSV types | `cluster-quota` (`PayloadTypeClusterQuota`) |
| Retention tables | `cluster_quota_recommendation_sets`, `daily_cluster_quota_digests` |

## Traits

| Trait | Supported |
|-------|-----------|
| CSVIngestor | Yes — `IngestCSV` → `ProcessClusterQuotaCSV` |
| IngestHook | No |
| APIProvider | Yes — `GET /recommendations/openshift/cluster-quota` |
| Settings | Yes — `GET/PUT/DELETE /recommendations/openshift/settings/cluster-quota` (registered in server when plugin enabled) |

## Endpoints

### List recommendations

```
GET /api/cost-management/v1/recommendations/openshift/cluster-quota/
```

**Query parameters:**

| Parameter | Description |
|-----------|-------------|
| `filter[cluster]` | Cluster UUID |
| `filter[cluster_quota_name]` | CRQ name (aliases: `filter[cluster_resource_quota]`, `filter[crq]`) |
| `filter[project]` | CRQs whose `namespaces` membership includes the value (`filter[namespace]` alias) |
| `filter[recommendation_type]` | `tighten`, `raise`, `optimal`, `none` |
| `filter[risk_level]` | `high`, `medium`, `low`, `none` |
| `filter[tag:<key>]` | Namespace tag filter when `ROS_TAGS_ENABLED=true` (CRQs whose selector includes a matching namespace) |
| `group_by[cluster]` | Aggregate per cluster (`count`, summed savings and capacity_freed) |
| `order_by` | `cluster_quota_name`, `utilization`, `risk_level`, `estimated_monthly_savings` |
| `order_how` | `asc` or `desc` |
| `limit` | Page size (1–100, default 20) |
| `offset` | Pagination offset |

### Detail

```
GET /api/cost-management/v1/recommendations/openshift/cluster-quota/detail?cluster_uuid=...&cluster_quota_name=...
```

Required params: `cluster_uuid`, `cluster_quota_name`. Returns one object with `history[]`
per resource. See [feature documentation](../features/cluster-resource-quota.md#detail-endpoint).

**Response fields (per list item):**

- `cluster_uuid`, `cluster_quota_name`
- `namespaces` — array of namespace names in the CRQ selector (from operator CSV `namespaces` column)
- `recommendation_type`, `risk_level`
- `quota_hard`, `quota_used`, `quota_recommended` — CPU/memory request and limit, storage request, pods
- `utilization` — `cpu_request_percent`, `memory_request_percent`, `storage_request_percent`, `pods_percent`
- `capacity_freed` — `cpu_cores_freed`, `memory_bytes`, `storage_request_bytes`, `pods_freed` (on tighten)
- `estimated_savings` — `value` (whole USD), `units` (CPU/memory/storage when cost data exists; pods not monetized)

ClusterResourceQuota savings are **excluded from fleet `savings-summary` totals** to avoid
double-counting container-level savings that quota recommendations encompass.

Handler: [`GetClusterQuotaRecommendations`](../../internal/api/handlers_cluster_quota_recs.go).

### Settings

Per-organization overrides for CRQ headroom and utilization risk bands. Resolution order:
**Settings API** → **`ROS_CLUSTER_QUOTA_*` env vars** → compiled defaults (10 / 90 / 70).
See [Configurability — ClusterResourceQuota](../architecture/configurability.md#clusterresourcequota)
for env vars, precedence, and lock behavior.

```
GET /api/cost-management/v1/recommendations/openshift/settings/cluster-quota
PUT /api/cost-management/v1/recommendations/openshift/settings/cluster-quota
DELETE /api/cost-management/v1/recommendations/openshift/settings/cluster-quota
```

Requires the `cluster-quota` plugin (`ROS_ENABLED_PLUGINS`). When the plugin is disabled,
routes return **404**.

#### GET response

```json
{
  "headroom_percent": 10,
  "high_risk_threshold_percent": 90,
  "medium_risk_threshold_percent": 70,
  "locked_fields": []
}
```

`locked_fields` lists API fields that cannot be changed via PUT because the matching
`ROS_CLUSTER_QUOTA_*` environment variable is set on the deployment.

| Field | Default | Purpose |
|-------|---------|---------|
| `headroom_percent` | `10` | Extra margin on recommended CRQ hard values (10 → 110% of aggregated namespace quota sums) |
| `high_risk_threshold_percent` | `90` | Triggers `raise` recommendation and `high` risk when utilization ≥ threshold |
| `medium_risk_threshold_percent` | `70` | `medium` risk when utilization is between medium and high thresholds |

#### PUT request

Send all three percent fields in the JSON body:

```json
{
  "headroom_percent": 15,
  "high_risk_threshold_percent": 85,
  "medium_risk_threshold_percent": 65
}
```

Validation rules:

- `headroom_percent`: 0–100
- `high_risk_threshold_percent`: 1–100, must be **greater than** `medium_risk_threshold_percent`
- `medium_risk_threshold_percent`: 1–99

Returns **400** with `validation_errors` on invalid input. Returns **403** when updating
a field listed in `locked_fields`.

#### DELETE

Removes the per-org override in `recommendation_thresholds` (`recommendation_type=cluster-quota`).
Subsequent runs use env or compiled defaults.

#### Environment locks

| Variable | Locks field |
|----------|-------------|
| `ROS_CLUSTER_QUOTA_HEADROOM_PERCENT` | `headroom_percent` |
| `ROS_CLUSTER_QUOTA_HIGH_RISK_THRESHOLD_PERCENT` | `high_risk_threshold_percent` |
| `ROS_CLUSTER_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT` | `medium_risk_threshold_percent` |

Handlers: [`GetClusterQuotaSettings`](../../internal/api/handlers_cluster_quota_settings.go),
[`PutClusterQuotaSettings`](../../internal/api/handlers_cluster_quota_settings.go),
[`DeleteClusterQuotaSettings`](../../internal/api/handlers_cluster_quota_settings.go).

Engine: [`ResolveClusterQuotaSettings`](../../internal/engine/cluster_quota_settings.go).

### Notification codes

CRQ rows may emit codes **70–73**. Filter the catalog with
`GET .../notification-codes/?filter[plugin]=cluster-quota`.

| Code | Name | Description |
|------|------|-------------|
| **70** | `QUOTA_NEAR_CAPACITY` | CRQ at high utilization risk |
| **71** | `QUOTA_OVERSIZED` | CRQ tighten recommendation generated |
| **72** | `QUOTA_BLOCKING` | CRQ resource blocking (used >= hard) |
| **73** | `CLUSTER_QUOTA_AT_CAPACITY` | CRQ at capacity alert |

Object-count resources (`count/deployments.apps`, `count/secrets`, etc.) contribute to
**risk_level** and **blocking notifications** (code 72) only. They are not exposed in API
`utilization` fields or used in `order_by=utilization`. See
[ClusterResourceQuota Recommendations](../features/cluster-resource-quota.md#object-count-quotas-risk-and-notifications-only).

### Savings recalculation

When Koku cost model rates change, masu triggers
`POST /api/cost-management/v1/internal/recalculate-savings` with
`recommendation_types` including `cluster-quota`. ROS recomputes `estimated_savings` on
existing tighten rows without re-ingestion. Requires `ROS_SAVINGS_RECALCULATION_ENABLED=true`.

## Engine

- Ingest: [`ProcessClusterQuotaCSV`](../../internal/ingestion/cluster_quota.go)
- Recommend: [`RunClusterQuotaRecommendations`](../../internal/engine/cluster_quota_run.go),
  [`RecommendClusterQuotas`](../../internal/engine/recommend_cluster_quota.go)
- Settings: [`ResolveClusterQuotaSettings`](../../internal/engine/cluster_quota_settings.go)

## Feature documentation

- [ClusterResourceQuota Recommendations](../features/cluster-resource-quota.md)
