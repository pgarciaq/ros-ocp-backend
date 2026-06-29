# Issue #3 Update: OOM Timeline Endpoint Implementation

## Summary

Implemented a dedicated OOM timeline endpoint as part of the Visual Insights
Dashboard (ADR-0301, Tier 1). This delivers per-day OOM kill event data for
container recommendations.

## Implementation Plan

### Backend (Completed)

1. **Model** (`internal/model/oom_timeline.go`):
   - `ResolveContainerKeyByID()` — resolves `recommendation-id` UUID to composite
     container key via `recommendation_sets.container_id`
   - `QueryOOMTimeline()` — queries `daily_container_digests` for days with
     `oom_count_sum > 0`, scoped by org_id and date range
   - Response structs: `OOMTimelineResponse`, `OOMTimelineMeta`, `OOMTimelineEntry`

2. **Handler** (`internal/api/handlers_oom_timeline.go`):
   - `GetOOMTimeline()` — Echo handler with x-rh-identity auth, UUID validation,
     date range parsing, container key resolution, and query execution
   - `parseOOMTimelineDateRange()` — extracts/validates optional `start_date` and
     `end_date` query params (defaults: 6 months ago → today)

3. **Route** (`internal/api/server.go`):
   - Registered at `/recommendations/openshift/containers/:recommendation-id/oom-timeline`
   - Behind `nativeRecommendationRoutes` guard (native engine only)
   - Positioned before the parameterized `/:recommendation-id` catch-all

4. **Tests** (`internal/api/handlers_oom_timeline_test.go`):
   - Happy path with mixed OOM/non-OOM days
   - Empty result (all OOM counts zero)
   - Container not found (404)
   - Invalid date range combinations (400)
   - Bad UUID format (400)
   - Default date range (no query params)
   - Missing identity header

5. **OpenAPI** (`openapi.json`):
   - Path definition with parameters and response schemas
   - `OOMTimelineResponse`, `OOMTimelineMeta`, `OOMTimelineEntry` component schemas

6. **ADR** (`docs/adr/0302-oom-timeline-endpoint.md`):
   - Documents Option B (dedicated endpoint) over Option A (extend boxplots)

## Endpoint Specification

```
GET /api/cost-management/v1/recommendations/openshift/containers/{recommendation-id}/oom-timeline
```

### Query Parameters

| Parameter    | Type   | Required | Default        | Description                     |
|--------------|--------|----------|----------------|---------------------------------|
| `start_date` | string | No       | 6 months ago   | ISO 8601 date (YYYY-MM-DD)      |
| `end_date`   | string | No       | today          | ISO 8601 date (YYYY-MM-DD)      |

### Response (200 OK)

```json
{
  "meta": {
    "count": 45,
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

### Error Responses

| Status | Condition                                    |
|--------|----------------------------------------------|
| 400    | Invalid UUID, invalid date format, start > end |
| 401    | Missing or invalid x-rh-identity header      |
| 404    | Container not found for the given org_id     |
| 503    | Database connection unavailable              |

### Key Design Decisions

- **Sparse response**: Only days with `oom_count_sum > 0` are returned. The
  frontend renders zero-event days as gaps in the timeline.
- **`meta.count`**: Sum of all `oom_kill_count` values (total events, not total days).
- **`schedule_type = 'all_hours'`**: Uses all-hours digests, not business-hours, since
  OOM events are relevant regardless of business schedule.

## Frontend Requirements

### OOM Timeline Chart

- **Component**: Scatter plot or bar chart on a date axis
- **Data source**: Lazy-fetch from `/containers/{id}/oom-timeline` when user
  expands the OOM section in the container detail view
- **Empty state**: Display "No OOM events detected in this period" message with
  a subtle icon when `data` is empty
- **Date range**: Default to full 6-month window; optionally expose date pickers
  for custom ranges
- **Interactivity**: Tooltip on each point showing date and kill count

### Integration Points

- Container recommendation detail page (existing route: `/{provider}/breakdown`)
- Should be a collapsible section below the main recommendation content
- Fetch should be triggered on section expand, not on page load (lazy loading)
