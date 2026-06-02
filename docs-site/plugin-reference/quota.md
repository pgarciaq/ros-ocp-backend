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
| Retention tables | `quota_recommendation_sets` |

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
| `filter[recommendation_type]` | `tighten`, `raise`, `optimal`, `none` |
| `filter[risk_level]` | `high`, `medium`, `low`, `none` |
| `group_by[cluster]` | Aggregate per cluster |
| `group_by[project]` | Aggregate per namespace |
| `limit` | Page size (1–100, default 20) |
| `offset` | Pagination offset |

Handler: [`GetQuotaRecommendations`](../../internal/api/handlers_quota_recs.go).

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

## Feature documentation

- [ResourceQuota Recommendations](../features/quota-recommendations.md)
- [OpenAPI specification](../openapi.md)
