# OOM Timeline API

> **Last verified:** 2026-09-14

Returns per-day OOM (Out of Memory) kill counts for a container recommendation.
Only days with at least one OOM event are included (sparse response).

**ADR:** [0302-oom-timeline-endpoint](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0302-oom-timeline-endpoint.md)

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
| `start_date` | string | No       | 6 months ago, capped by `ROS_MAX_LOOKBACK_DAYS` (default 90) | ISO 8601 date (`YYYY-MM-DD`)    |
| `end_date`   | string | No       | today        | ISO 8601 date (`YYYY-MM-DD`)    |

Explicit date ranges are limited to **90 days** span (`maxSpanDays` in
`parseOOMTimelineDateRange`) **and** to `ROS_MAX_LOOKBACK_DAYS` (default 90;
invalid values fall back to 14). With default configuration the effective
default window is therefore the last 90 days.

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
| 400    | Date range wider than 90 days                           | `"date range must not exceed 90 days"`               |
| 400    | Explicit range wider than `ROS_MAX_LOOKBACK_DAYS`       | `"date range exceeds maximum of 90 days"` (limit follows config) |
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

## Live verification (2026-09-14)

OOM data depends on fixtures (`daily_container_digests.oom_count_sum > 0` with
`schedule_type = 'all_hours'`). Local org `3340851` has no OOM events, so the
live response is empty-with-shape — this is the expected shape when no OOM kills
occurred, not an error:

```json
{
  "meta": {
    "count": 0,
    "container_id": "58015f8f-ab9e-51db-abe0-bf9bfe63096c",
    "start_date": "2026-08-01",
    "end_date": "2026-09-14"
  },
  "data": []
}
```

Recorded via
`GET .../containers/58015f8f-ab9e-51db-abe0-bf9bfe63096c/oom-timeline?start_date=2026-08-01&end_date=2026-09-14`
(default range returned `start_date: 2026-06-16` — 90 days back per
`ROS_MAX_LOOKBACK_DAYS`). Error rows above were verified live against the same
container: bad UUID → `"bad recommendation-id"`, reversed range →
`"start_date must not be after end_date"`, 256-day range →
`"date range must not exceed 90 days"`, unknown UUID → 404 `"container not found"`.

---

## Data Source

Reads from `daily_container_digests.oom_count_sum` (collected by the
koku-metrics-operator via Prometheus `kube_pod_container_status_restarts_total`
with OOM reason filter). Only `schedule_type = 'all_hours'` rows are queried.

## Related

- [Visual Insights Dashboard (ADR-0301)](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/adr/0301-visual-insights-dashboard.md)
- [Container Recommendations](../plugin-reference/container.md)
- [OpenAPI Spec](../openapi.md)
