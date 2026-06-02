# ClusterResourceQuota Recommendations

**Status:** **IMPLEMENTED** (Phase 10)  
**Plugin:** `cluster-quota` (Phase 1, priority 36)  
**OpenShift:** Required — `ClusterResourceQuota` is an OpenShift API extension (`quota.openshift.io/v1`).

ClusterResourceQuota enforces hard/used quota semantics aggregated across multiple namespaces
selected by a label or annotation selector. ROS compares CRQ metrics from
`openshift_clusterresourcequota_usage` against summed namespace ResourceQuota recommendations.

---

## Endpoints

### List

```
GET /api/cost-management/v1/recommendations/openshift/cluster-quota/
```

| Query param | Maps to |
|-------------|---------|
| `filter[cluster]` | `cluster_uuid` |
| `filter[cluster_quota_name]`, `filter[cluster_resource_quota]`, or `filter[crq]` | `cluster_quota_name` |
| `filter[recommendation_type]` | `tighten` \| `raise` \| `optimal` \| `none` |
| `filter[risk_level]` | `high` \| `medium` \| `low` \| `none` |
| `filter[namespace]` or `filter[project]` | CRQs whose namespace membership includes the value |
| `group_by[cluster]` | Aggregate per cluster (sum `capacity_freed` and `estimated_savings`; row includes `count`) |
| `order_by`, `order_how` | Sort — see [order_by values](#order_by-values) |
| `limit`, `offset` | Pagination (default limit 20, max 100) |

**Response fields (per item):**

- `cluster_uuid`, `cluster_quota_name`
- `recommendation_type`, `risk_level`
- `quota_hard`, `quota_used`, `quota_recommended` — CPU/memory request and limit, storage request, pods
- `utilization` — `cpu_request_percent`, `memory_request_percent`, `storage_request_percent`, `pods_percent`
- `capacity_freed` — `cpu_cores_freed`, `memory_bytes`, `storage_request_bytes`, `pods_freed` (on tighten)
- `estimated_savings` — `value` (whole USD), `units`
- `notifications` — map of notification entries (codes 70–73)
- `namespaces` — namespace membership when operator exports the column

Handler: [`GetClusterQuotaRecommendations`](../../internal/api/handlers_cluster_quota_recs.go).

### Detail

```
GET /api/cost-management/v1/recommendations/openshift/cluster-quota/detail?cluster_uuid=...&cluster_quota_name=...
```

| Query param | Required | Description |
|-------------|----------|-------------|
| `cluster_uuid` | Yes | Cluster UUID |
| `cluster_quota_name` | Yes | CRQ object name |

Returns **one** CRQ recommendation object (not wrapped in `data[]`) with the same fields as a
list row plus `history[]` — an array of per-resource snapshots for trend charts:

```json
{
  "cluster_uuid": "550e8400-e29b-41d4-a716-446655440001",
  "cluster_quota_name": "team-payments-quota",
  "recommendation_type": "tighten",
  "risk_level": "low",
  "quota_hard": { "cpu_request_millicores": 500000 },
  "history": [
    {
      "recorded_at": "2026-05-15T12:00:00Z",
      "resource": "cpu_request",
      "recommendation_type": "tighten",
      "risk_level": "low",
      "recommended_hard": 396000,
      "current_hard": 500000,
      "current_used": 175000,
      "utilization_percent": 35
    }
  ]
}
```

Handler: [`GetClusterQuotaRecommendationDetail`](../../internal/api/handlers_quota_detail.go).

### Settings

```
GET /api/cost-management/v1/recommendations/openshift/settings/cluster-quota
PUT /api/cost-management/v1/recommendations/openshift/settings/cluster-quota
DELETE /api/cost-management/v1/recommendations/openshift/settings/cluster-quota
```

See [plugin reference — cluster-quota settings](../plugin-reference/cluster-quota.md#settings).

---

## group_by[cluster]

Aggregate CRQ recommendations per cluster. Mutually exclusive with per-CRQ list fields such
as `cluster_quota_name`.

```
GET /api/cost-management/v1/recommendations/openshift/cluster-quota/?group_by[cluster]=*
```

Example response snippet:

```json
{
  "meta": { "count": 2, "limit": 20, "offset": 0 },
  "data": [
    {
      "cluster_uuid": "550e8400-e29b-41d4-a716-446655440001",
      "count": 3,
      "capacity_freed": {
        "cpu_cores_freed": 12,
        "memory_bytes": 85899345920,
        "storage_request_bytes": 10737418240,
        "pods_freed": 5
      },
      "estimated_savings": { "value": 840, "units": "USD" }
    }
  ]
}
```

---

## order_by values

| `order_by` | Sort key |
|------------|----------|
| `cluster_quota_name` | CRQ object name (default) |
| `utilization` | Max of CPU/memory/storage/pods utilization percents |
| `risk_level` | `high` → `medium` → `low` → `none` |
| `estimated_monthly_savings` | `savings_dollars_monthly` (tighten rows only) |

Use `order_how=asc` or `order_how=desc` (default `desc` when `order_by` is set).

---

## Notification codes

CRQ rows may emit codes **70–73**. Filter the catalog:
`GET /api/cost-management/v1/notification-codes/?filter[plugin]=cluster-quota`.

| Code | Name | When emitted (CRQ) |
|------|------|-------------------|
| **70** | `QUOTA_NEAR_CAPACITY` | `risk_level` is `high` |
| **71** | `QUOTA_OVERSIZED` | `recommendation_type` is `tighten` |
| **72** | `QUOTA_BLOCKING` | `used >= hard` on any tracked resource |
| **73** | `CLUSTER_QUOTA_AT_CAPACITY` | `risk_level` is `high` (CRQ-specific; often co-emitted with **70**) |

Implementation: [`ClusterQuotaNotificationCodes`](../../internal/engine/quota_notifications.go).

---

## Object-count quotas (risk and notifications only)

The operator ingests aggregated `object_count_*` metrics (sum of Kubernetes `count/*`
resources). These are **not** exposed in API `utilization` fields or `order_by=utilization`.
They **do** contribute to `risk_level` classification and can trigger notifications:

- Code **72** when `used >= hard` on object counts
- Codes **70** and **73** when overall CRQ `risk_level` is `high`

Object counts do not produce tighten/raise recommendations or `estimated_savings`.

See the full rationale in [internal feature doc](../../docs/features/cluster-resource-quota.md#object-count-quotas-risk-and-notifications-only).

---

## Enablement

Include `cluster-quota` in `ROS_ENABLED_PLUGINS`. When disabled, list, detail, and settings
routes return **404**.

OpenAPI: [openapi.json](../../openapi.json). Internal architecture:
[docs/features/cluster-resource-quota.md](../../docs/features/cluster-resource-quota.md).
