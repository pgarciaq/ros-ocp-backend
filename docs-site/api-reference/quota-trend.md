# Quota Headroom Trend API

> **Last verified:** 2026-08-06

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

### Query Parameters

| Parameter    | Type   | Required | Default      | Description                     |
|--------------|--------|----------|--------------|---------------------------------|
| `start_date` | string | No       | 30 days ago  | ISO 8601 date (`YYYY-MM-DD`)    |
| `end_date`   | string | No       | today        | ISO 8601 date (`YYYY-MM-DD`)    |

---

## Response

### 200 OK

```json
{
  "meta": {
    "count": 5,
    "cluster_uuid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "namespace": "my-project",
    "start_date": "2026-06-01",
    "end_date": "2026-06-05"
  },
  "data": [
    {
      "date": "2026-06-01",
      "cpu_request_hard_millicores": 4000,
      "cpu_request_used_millicores": 2500,
      "memory_request_hard_bytes": 8589934592,
      "memory_request_used_bytes": 4294967296
    },
    {
      "date": "2026-06-02",
      "cpu_request_hard_millicores": 4000,
      "cpu_request_used_millicores": 2800,
      "memory_request_hard_bytes": 8589934592,
      "memory_request_used_bytes": 5368709120
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

When no quota data exists for the date range:

```json
{
  "meta": {
    "count": 0,
    "cluster_uuid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "namespace": "my-project",
    "start_date": "2026-06-01",
    "end_date": "2026-06-30"
  },
  "data": []
}
```

### Error Responses

| Status | Condition                                               | Example message                                      |
|--------|---------------------------------------------------------|------------------------------------------------------|
| 400    | Invalid UUID format                                     | `"bad quota-id"`                                     |
| 400    | Invalid date format                                     | `"invalid start_date: must be ISO 8601 date (YYYY-MM-DD)"` |
| 400    | `start_date` after `end_date`                           | `"start_date must not be after end_date"`            |
| 401    | Missing or invalid `x-rh-identity` header               | `"missing or invalid identity"`                      |
| 404    | Quota recommendation not found for the authenticated org | `"quota recommendation not found"`                   |
| 503    | Database connection unavailable                         | `"database connection unavailable"`                  |

---

## Example

```bash
IDENTITY=$(echo -n '{"identity":{"account_number":"10001","org_id":"1234567","type":"User","user":{"username":"user_dev","email":"user_dev@foo.com","is_org_admin":true,"access":{}}},"entitlements":{"cost_management":{"is_entitled":true}}}' | base64 -w0)

curl -s -H "x-rh-identity: $IDENTITY" \
  'http://localhost:8000/api/cost-management/v1/recommendations/openshift/quota/a1b2c3d4-e5f6-7890-abcd-ef1234567890/trend?start_date=2026-06-01&end_date=2026-06-30' \
  | python3 -m json.tool
```

---

## Data Source

Reads from `daily_namespace_quota_digests` which stores daily snapshots of
`ResourceQuota` hard limits and actual usage per namespace. Data is collected
by the koku-metrics-operator via Prometheus queries against
`kube_resourcequota` metrics.

## Related

- [Visual Insights Dashboard (ADR-0301)](../../docs/adr/0301-visual-insights-dashboard.md)
- [Quota Recommendations](../plugin-reference/quota.md)
- [OpenAPI Spec](../openapi.md)
