# quota

Package: [`internal/plugins/quota`](../../internal/plugins/quota/)

Namespace **ResourceQuota** right-sizing recommendations. Compares quota hard/used metrics
from namespace ROS CSVs against aggregated container recommendation totals.

## Plugin metadata

| Property | Value |
|----------|-------|
| Name | `quota` |
| Phase | 1 (Produce) |
| Priority | 35 |
| CSV types | (none — runs after container ingest in `report_processor`) |
| Retention tables | `quota_recommendation_sets`, `quota_recommendation_history` |

## Traits

| Trait | Supported |
|-------|-----------|
| CSVIngestor | No — recommendations run in `processContainerCSVNative` after container `recommendation_sets` are written |
| IngestHook | No |
| APIProvider | Yes — `GET /recommendations/openshift/quota` |
| Settings | Yes — `GET/PUT/DELETE /recommendations/openshift/settings/quota` (registered in server when plugin enabled) |

## Endpoints

### List recommendations

```
GET /api/cost-management/v1/recommendations/openshift/quota/
```

**Query parameters:**

| Parameter | Description |
|-----------|-------------|
| `filter[cluster]` | Cluster UUID |
| `filter[project]` | Namespace |
| `filter[quota_name]` | ResourceQuota object name (exact match). Alias: `filter[resource_quota_name]` |
| `filter[recommendation_type]` | `tighten`, `raise`, `optimal`, `none` |
| `filter[risk_level]` | `high`, `medium`, `low`, `none` |
| `group_by[cluster]` | Aggregate per cluster (`*`) |
| `group_by[project]` | Aggregate per namespace (`*`) |
| `order_by` | `namespace`, `quota_name`, `utilization`, `estimated_monthly_savings`, `risk_level` |
| `order_how` | `asc` or `desc` (default `desc` when `order_by` is set) |
| `limit` | Page size (1–100, default 20) |
| `offset` | Pagination offset |
| `format` | `json` (default) or `csv` — flattened list export (`Accept: text/csv` also supported) |
| `filter[tag:<key>]` | Namespace tag filter (requires `ROS_TAGS_ENABLED=true`) |

Handler: [`GetQuotaRecommendations`](../../internal/api/handlers_quota_recs.go).

#### CSV export

```
GET /api/cost-management/v1/recommendations/openshift/quota/?format=csv&limit=100
```

Returns `text/csv` with 37 columns including `cluster_uuid`, `namespace`, `quota_name`,
`recommendation_type`, `risk_level`, full `quota_hard_*` / `quota_used_*` /
`quota_recommended_*` resource blocks (CPU request/limit millicores, memory request/limit
bytes, storage request bytes, pods), `utilization_*_percent` columns,
`capacity_freed_*` columns, `estimated_savings_value`, `estimated_savings_units`,
`last_observed_at`, `notification_codes`, `count`. Same filters, sort, and pagination as
the JSON list.

#### RBAC

When `RBAC_ENABLE=true`, the list endpoint scopes results to clusters the caller may read
(`openshift.cluster` permission). `filter[cluster]` for an unknown or unauthorized cluster
UUID returns **200** with an empty `data` array (not 403), consistent with other ROS list APIs.
Project-level filtering uses `openshift.project` where configured.

ResourceQuota recommendations are **persisted** with `term=medium` and `engine=cost` container
aggregates. List and detail endpoints accept optional `filter[term]` / `filter[engine]` (or flat
`term` / `engine`) to **reproject** recommendations on read using container aggregates for the
requested term/engine. When omitted, defaults match persistence (`medium` + `cost`). ClusterResourceQuota
list/detail use the same projection parameters; CRQ values are derived from reprojected namespace
quota sums for namespaces in scope.

### Detail

```
GET /api/cost-management/v1/recommendations/openshift/quota/detail
  ?cluster_uuid={uuid}&namespace={ns}&quota_name={name}
```

Returns one recommendation object (not wrapped in `data`) with `headroom_basis_points`,
`notifications` (codes **70–72**), and `history[]` per-resource snapshots (`recorded_at`,
`resource`, `recommendation_type`, `risk_level`, and optional hard/used/utilization fields).
`quota_name` is optional when only one ResourceQuota exists for the namespace.

Handler: [`GetQuotaRecommendationDetail`](../../internal/api/handlers_quota_detail.go).

### Notification codes

Namespace ResourceQuota rows may emit codes **70–72**:

| Code | Name | When |
|------|------|------|
| **70** | `NotifQuotaNearCapacity` | `risk_level` is `high` |
| **71** | `NotifQuotaOversized` | `recommendation_type` is `tighten` |
| **72** | `NotifQuotaBlocking` | Any resource at or above hard limit |

Filter the catalog: `GET /recommendations/openshift/notification-codes?filter[plugin]=quota`.
Code **73** applies to the `cluster-quota` plugin only.

### Settings

Per-organization overrides for ResourceQuota headroom and utilization risk bands. Resolution
order: **Settings API** → **`ROS_QUOTA_*` env vars** → compiled defaults (10 / 90 / 70).
See [Configurability — ResourceQuota](../architecture/configurability.md#resourcequota) for
env vars, precedence, and lock behavior.

```
GET /api/cost-management/v1/recommendations/openshift/settings/quota
PUT /api/cost-management/v1/recommendations/openshift/settings/quota
DELETE /api/cost-management/v1/recommendations/openshift/settings/quota
```

Requires the `quota` plugin (`ROS_ENABLED_PLUGINS`). When the plugin is disabled, routes
return **404**.

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
`ROS_QUOTA_*` environment variable is set on the deployment.

| Field | Default | Purpose |
|-------|---------|---------|
| `headroom_percent` | `10` | Extra margin on recommended hard values (10 → 110% of container rec sums) |
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

Removes the per-org override in `recommendation_thresholds` (`recommendation_type=quota`).
Subsequent runs use env or compiled defaults.

#### Environment locks

| Variable | Locks field |
|----------|-------------|
| `ROS_QUOTA_HEADROOM_PERCENT` | `headroom_percent` |
| `ROS_QUOTA_HIGH_RISK_THRESHOLD_PERCENT` | `high_risk_threshold_percent` |
| `ROS_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT` | `medium_risk_threshold_percent` |

Handlers: [`GetQuotaSettings`](../../internal/api/handlers_quota_settings.go),
[`PutQuotaSettings`](../../internal/api/handlers_quota_settings.go),
[`DeleteQuotaSettings`](../../internal/api/handlers_quota_settings.go).

Engine: [`ResolveQuotaSettings`](../../internal/engine/quota_settings.go).

## Engine

- Recommend: [`RunQuotaRecommendations`](../../internal/engine/quota_run.go),
  [`RecommendQuotas`](../../internal/engine/recommend_quota.go)
- Settings: [`ResolveQuotaSettings`](../../internal/engine/quota_settings.go)

## Notes

Quota savings are excluded from the fleet-level
`GET /api/cost-management/v1/recommendations/openshift/savings-summary` endpoint to avoid
double-counting with container-level savings that already account for quota-bound workloads.

## Feature documentation

- [ResourceQuota Recommendations](../features/quota-recommendations.md)
- [OpenAPI specification](../openapi.md)
