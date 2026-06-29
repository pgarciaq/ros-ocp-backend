# OOM Timeline API

Returns per-day OOM (Out of Memory) kill counts for a container recommendation.
Only days with at least one OOM event are included (sparse response).

**ADR:** [0302-oom-timeline-endpoint](../../docs/adr/0302-oom-timeline-endpoint.md)

---

## Endpoint

```
GET /api/cost-management/v1/recommendations/openshift/containers/{recommendation-id}/oom-timeline
```

### Path Parameters

| Parameter           | Type   | Description                                      |
|---------------------|--------|--------------------------------------------------|
| `recommendation-id` | string | Container recommendation UUID (deterministic v5) |

### Query Parameters

| Parameter    | Type   | Required | Default      | Description                     |
|--------------|--------|----------|--------------|---------------------------------|
| `start_date` | string | No       | 6 months ago | ISO 8601 date (`YYYY-MM-DD`)    |
| `end_date`   | string | No       | today        | ISO 8601 date (`YYYY-MM-DD`)    |

---

## Response

### 200 OK

```json
{
  "meta": {
    "count": 9,
    "container_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "start_date": "2026-01-01",
    "end_date": "2026-06-29"
  },
  "data": [
    {"date": "2026-03-15", "oom_kill_count": 3},
    {"date": "2026-03-22", "oom_kill_count": 1},
    {"date": "2026-04-01", "oom_kill_count": 5}
  ]
}
```

| Field                  | Type    | Description                                         |
|------------------------|---------|-----------------------------------------------------|
| `meta.count`           | integer | Sum of all `oom_kill_count` values (total events)   |
| `meta.container_id`    | string  | The `recommendation-id` from the request path       |
| `meta.start_date`      | string  | Start of queried range (ISO 8601 date)              |
| `meta.end_date`        | string  | End of queried range (ISO 8601 date)                |
| `data[].date`          | string  | Date of OOM events (ISO 8601 date)                  |
| `data[].oom_kill_count`| integer | Number of OOM kills on this date (always ≥ 1)       |

### Empty Response

When no OOM events occurred in the date range:

```json
{
  "meta": {
    "count": 0,
    "container_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "start_date": "2026-01-01",
    "end_date": "2026-06-29"
  },
  "data": []
}
```

### Error Responses

| Status | Condition                                               | Example message                                      |
|--------|---------------------------------------------------------|------------------------------------------------------|
| 400    | Invalid UUID format                                     | `"bad recommendation-id"`                            |
| 400    | Invalid date format                                     | `"invalid start_date: must be ISO 8601 date (YYYY-MM-DD)"` |
| 400    | `start_date` after `end_date`                           | `"start_date must not be after end_date"`            |
| 401    | Missing or invalid `x-rh-identity` header               | `"missing or invalid identity"`                      |
| 404    | Container not found for the authenticated org           | `"container not found"`                              |
| 503    | Database connection unavailable                         | `"database connection unavailable"`                  |

---

## Example

```bash
IDENTITY=$(echo -n '{"identity":{"account_number":"10001","org_id":"1234567","type":"User","user":{"username":"user_dev","email":"user_dev@foo.com","is_org_admin":true,"access":{}}},"entitlements":{"cost_management":{"is_entitled":true}}}' | base64 -w0)

curl -s -H "x-rh-identity: $IDENTITY" \
  'http://localhost:8000/api/cost-management/v1/recommendations/openshift/containers/a1b2c3d4-e5f6-7890-abcd-ef1234567890/oom-timeline?start_date=2026-01-01&end_date=2026-06-29' \
  | python3 -m json.tool
```

---

## Data Source

Reads from `daily_container_digests.oom_count_sum` (collected by the
koku-metrics-operator via Prometheus `kube_pod_container_status_restarts_total`
with OOM reason filter). Only `schedule_type = 'all_hours'` rows are queried.

## Related

- [Visual Insights Dashboard (ADR-0301)](../../docs/adr/0301-visual-insights-dashboard.md)
- [Container Recommendations](../plugin-reference/container.md)
- [OpenAPI Spec](../openapi.md)
