# Quota Headroom Trend API

> **Last verified:** 2026-09-14

Returns per-day quota hard limit vs actual used values for CPU request and memory
request, enabling a headroom trend chart. The gap between hard and used represents
how much capacity remains before the namespace hits its quota ceiling.

---

## Endpoint

```
GET /api/cost-management/v1/recommendations/openshift/quota/{quota-id}/trend
```

### Path Parameters

| Parameter  | Type   | Description                                        |
|------------|--------|----------------------------------------------------|
| `quota-id` | string | Quota recommendation UUID (deterministic UUID v5)  |

Get a real `quota-id` from the list endpoint first
(`GET /api/cost-management/v1/recommendations/openshift/quota` returns `data[].id`).
The ID resolves to the `(cluster_uuid, namespace, quota_name)` composite key in
`quota_recommendation_sets` (handler: `GetQuotaTrend` in
`internal/api/handlers_quota_trend.go`, key resolution in
`internal/model/quota_trend.go` `ResolveQuotaKeyByID`).

### Query Parameters

| Parameter    | Type   | Required | Default      | Description                     |
|--------------|--------|----------|--------------|---------------------------------|
| `start_date` | string | No       | 30 days ago  | ISO 8601 date (`YYYY-MM-DD`)    |
| `end_date`   | string | No       | today        | ISO 8601 date (`YYYY-MM-DD`)    |

#### Range limits

Two guards apply (both verified against code; the 90-day guard also triggered live):

- Hardcoded span cap in `parseQuotaTrendDateRange`
  (`internal/api/handlers_quota_trend.go`): `date range must not exceed 90 days`
  whenever `end_date - start_date > 90 days`, regardless of which dates are used.
- Configurable `ROS_MAX_LOOKBACK_DAYS` (`internal/config/config.go`, default `90`,
  fallback `14` when invalid): enforced **only when the caller passes explicit
  `start_date`/`end_date`**. With default config both guards agree at 90 days.

---

## Response

### 200 OK

Recorded live on 2026-09-14 against local data (org `3340851`, cluster
`550e8400-e29b-41d4-a716-446655440001`, namespace `analytics`, quota-id
`a8d9e2f1-48e3-5496-8f2e-1734adfd4874`; trimmed to a 3-day window for readability —
the default range returned 28 points, `2026-08-15` to `2026-09-14`):

```json
{
  "meta": {
    "count": 3,
    "cluster_uuid": "550e8400-e29b-41d4-a716-446655440001",
    "namespace": "analytics",
    "start_date": "2026-08-10",
    "end_date": "2026-08-12"
  },
  "data": [
    {
      "date": "2026-08-10",
      "cpu_request_hard_millicores": 8000,
      "cpu_request_used_millicores": 7528,
      "memory_request_hard_bytes": 12884901888,
      "memory_request_used_bytes": 12238008320
    },
    {
      "date": "2026-08-11",
      "cpu_request_hard_millicores": 8000,
      "cpu_request_used_millicores": 7527,
      "memory_request_hard_bytes": 12884901888,
      "memory_request_used_bytes": 12201147392
    },
    {
      "date": "2026-08-12",
      "cpu_request_hard_millicores": 8000,
      "cpu_request_used_millicores": 7597,
      "memory_request_hard_bytes": 12884901888,
      "memory_request_used_bytes": 12239878144
    }
  ]
}
```

| Field                                | Type    | Description                                         |
|--------------------------------------|---------|-----------------------------------------------------|
| `meta.count`                         | integer | Number of daily data points in the response         |
| `meta.cluster_uuid`                  | string  | Cluster UUID for the quota                          |
| `meta.namespace`                     | string  | Namespace scoped by the quota                       |
| `meta.start_date`                    | string  | Start of queried range (ISO 8601 date)              |
| `meta.end_date`                      | string  | End of queried range (ISO 8601 date)                |
| `data[].date`                        | string  | Report date (ISO 8601 date)                         |
| `data[].cpu_request_hard_millicores` | integer | CPU request quota hard limit in millicores          |
| `data[].cpu_request_used_millicores` | integer | CPU request actual usage in millicores              |
| `data[].memory_request_hard_bytes`   | integer | Memory request quota hard limit in bytes            |
| `data[].memory_request_used_bytes`   | integer | Memory request actual usage in bytes                |

Values are nullable — `null` means the metric was not collected on that date.

### Empty Response

When no quota data exists for the date range (shape verified against handler +
`QueryQuotaTrend`; sparse local data returns `data: []` with the echoed range):

```json
{
  "meta": {
    "count": 0,
    "cluster_uuid": "550e8400-e29b-41d4-a716-446655440001",
    "namespace": "analytics",
    "start_date": "2026-06-01",
    "end_date": "2026-06-30"
  },
  "data": []
}
```

### Error Responses

All rows below were triggered honestly against the live local API on 2026-09-14,
except the 503 row which is handler-verified (`database connection unavailable` /
`unable to fetch quota trend data` in `GetQuotaTrend`) and was not forced locally.

| Status | Condition                                               | Example message                                      |
|--------|---------------------------------------------------------|------------------------------------------------------|
| 400    | Invalid UUID format                                     | `"bad quota-id"`                                     |
| 400    | Invalid date format (`start_date` or `end_date`)        | `"invalid start_date: must be ISO 8601 date (YYYY-MM-DD)"` |
| 400    | `start_date` after `end_date`                           | `"start_date must not be after end_date"`            |
| 400    | Date range wider than 90 days                           | `"date range must not exceed 90 days"`               |
| 401    | Missing or invalid `x-rh-identity` header               | `"Unable to unmarshal X-Rh-Identity into struct"`    |
| 404    | Quota recommendation not found for the authenticated org | `"quota recommendation not found"`                   |
| 503    | Database connection unavailable (handler-verified)      | `"database connection unavailable"`                  |

The 90-day guard was triggered live with both
`start_date=2025-01-01&end_date=2026-09-14` and
`start_date=2026-01-01&end_date=2026-09-14` (HTTP 400,
`"date range must not exceed 90 days"`). The 404 was triggered with a well-formed
but unknown UUID (`11111111-1111-1111-1111-111111111111`).

---

## Example

List quotas first to get a real `quota-id`, then fetch its trend
(recorded 2026-09-14; org `3340851` has Aug–Sep 2026 digests):

```bash
IDENTITY=$(echo -n '{"identity":{"account_number":"10001","org_id":"3340851","type":"User","user":{"username":"admin","email":"admin@example.com","is_org_admin":true}},"entitlements":{"cost_management":{"is_entitled":true}}}' | base64 -w0)

curl -s -H "x-rh-identity: $IDENTITY" \
  'http://localhost:8000/api/cost-management/v1/recommendations/openshift/quota?limit=5' \
  | python3 -m json.tool
# data[].id -> e.g. a8d9e2f1-48e3-5496-8f2e-1734adfd4874 (analytics)

curl -s -H "x-rh-identity: $IDENTITY" \
  'http://localhost:8000/api/cost-management/v1/recommendations/openshift/quota/a8d9e2f1-48e3-5496-8f2e-1734adfd4874/trend?start_date=2026-08-10&end_date=2026-08-12' \
  | python3 -m json.tool
```

---

## Data Source

Reads from `daily_namespace_quota_digests` which stores daily snapshots of
`ResourceQuota` hard limits and actual usage per namespace. Data is collected
by the koku-metrics-operator via Prometheus queries against
`kube_resourcequota` metrics.

## Related

- [Visual Insights Dashboard (ADR-0301)](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0301-visual-insights-dashboard.md)
- [Quota Recommendations](../plugin-reference/quota.md)
- [OpenAPI Spec](../openapi.md)
