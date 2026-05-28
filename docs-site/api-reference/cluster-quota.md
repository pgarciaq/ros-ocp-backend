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
| `filter[recommendation_type]` | `tighten`, `raise`, `optimal`, `none` |
| `filter[risk_level]` | `high`, `medium`, `low`, `none` |
| `limit` | Page size (1–100, default 20) |
| `offset` | Pagination offset |

**Response fields (per item):**

- `cluster_uuid`, `cluster_quota_name`
- `recommendation_type`, `risk_level`
- `quota_hard`, `quota_used`, `quota_recommended` — CPU/memory request and limit millicores/bytes
- `utilization` — `cpu_request_percent`, `memory_request_percent`
- `capacity_freed` — `cpu_cores_freed`, `memory_bytes` (on tighten)
- `estimated_savings` — `value` (whole USD), `units` (when savings enabled)

Handler: [`GetClusterQuotaRecommendations`](../../internal/api/handlers_cluster_quota_recs.go).

### Settings

```
GET /api/cost-management/v1/recommendations/openshift/settings/cluster-quota
PUT /api/cost-management/v1/recommendations/openshift/settings/cluster-quota
DELETE /api/cost-management/v1/recommendations/openshift/settings/cluster-quota
```

Body fields: `headroom_percent`, `high_risk_threshold_percent`, `medium_risk_threshold_percent`.
GET includes `locked_fields` when `ROS_CLUSTER_QUOTA_*` env vars are set.

Handlers: [`GetClusterQuotaSettings`](../../internal/api/handlers_cluster_quota_settings.go), etc.

## Engine

- Ingest: [`ProcessClusterQuotaCSV`](../../internal/ingestion/cluster_quota.go)
- Recommend: [`RunClusterQuotaRecommendations`](../../internal/engine/cluster_quota_run.go),
  [`RecommendClusterQuotas`](../../internal/engine/recommend_cluster_quota.go)
- Settings: [`ResolveClusterQuotaSettings`](../../internal/engine/cluster_quota_settings.go)

## Feature documentation

- [ClusterResourceQuota Recommendations](../features/cluster-resource-quota.md)
- [OpenAPI specification](../openapi.md)
