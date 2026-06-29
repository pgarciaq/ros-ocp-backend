# ADR-0302: OOM Timeline Endpoint

## Status

Accepted

## Phase

Phase 17

## Context

The Visual Insights Dashboard (ADR-0301) identifies OOM kill history as a
Tier 1 chart: the data already exists in `daily_container_digests.oom_count_sum`
and needs only a thin API surface to reach the frontend.

Two options were evaluated:

### Option A: Extend Existing Boxplot Endpoint

Add an `oom_timeline` field to the existing
`GET /recommendations/openshift/containers/{id}` detail response (or boxplot
sub-resource). Pros: fewer endpoints. Cons:

- **Semantic mismatch.** Boxplots are tied to recommendation terms (short/medium/long)
  and scoped to the term's lookback window. OOM history is observability data that
  should cover the full retention window (potentially 1–3 years), independent of the
  selected term.
- **Coupling.** Changing the term selector would re-fetch OOM data unnecessarily.
  Conversely, fetching OOM data would require specifying a term even though it has
  no effect on the result.
- **Response bloat.** OOM data for 180+ days would increase the already large detail
  response size even when the user hasn't opened the OOM chart.

### Option B: Dedicated Endpoint

`GET /recommendations/openshift/containers/{id}/oom-timeline` with optional
`start_date` / `end_date` query parameters. The frontend fetches it lazily
(e.g., when the user expands the OOM section).

## Decision

Adopt **Option B**: a dedicated endpoint scoped to OOM kill data.

### Endpoint Specification

```
GET /api/cost-management/v1/recommendations/openshift/containers/{recommendation-id}/oom-timeline
```

**Query parameters:**

| Parameter    | Type | Default          | Description                        |
|--------------|------|------------------|------------------------------------|
| `start_date` | date | 6 months ago     | Earliest date to include           |
| `end_date`   | date | today            | Latest date to include             |

**Response (200):**

```json
{
  "meta": {
    "count": 45,
    "container_id": "uuid",
    "start_date": "2026-01-01",
    "end_date": "2026-06-29"
  },
  "data": [
    {"date": "2026-03-15", "oom_kill_count": 3},
    {"date": "2026-03-22", "oom_kill_count": 1}
  ]
}
```

- `data` is sparse: only days with `oom_count_sum > 0` are included.
- `meta.count` is the sum of all `oom_kill_count` values.
- Empty response (`data: []`, `count: 0`) when no OOMs occurred.

### SQL Query

```sql
SELECT bucket_date, oom_count_sum
FROM daily_container_digests
WHERE org_id = $1 AND cluster_uuid = $2
  AND namespace = $3 AND workload = $4
  AND workload_type = $5 AND container_name = $6
  AND bucket_date >= $7 AND bucket_date <= $8
  AND oom_count_sum > 0
  AND schedule_type = 'all_hours'
ORDER BY bucket_date ASC
```

The composite container key is resolved from the `recommendation-id` UUID via
`recommendation_sets.container_id` (added in migration 000030).

### Implementation

- Handler: `internal/api/handlers_oom_timeline.go` (~90 lines)
- Model: `internal/model/oom_timeline.go` (~100 lines)
- Route: registered in `server.go` before the parameterized `/:recommendation-id` catch-all
- Auth: standard `ros_middleware.Identity` + `CostManagementEntitlement` + optional RBAC

## Consequences

### Positive

- **Decoupled from term selector.** OOM timeline is independent of short/medium/long
  term settings, so it can show the full retention window.
- **Lazy loading.** Frontend only fetches OOM data when the user needs it, keeping
  the main detail response lean.
- **Future-proof.** When retention extends to 1–3 years, the endpoint handles it
  without affecting the boxplot payload size.
- **Minimal scope.** ~200 lines of Go total, no migrations, no schema changes.

### Negative

- **One more endpoint** to document and test. Mitigated by following existing patterns
  (namespace history, GPU MIG detail) and the small surface area.

### Neutral

- `Cache-Control: no-store` is set on the response (matching other recommendation
  data endpoints) to ensure the frontend always gets fresh data.

## Alternatives Considered

- **Option A** (extend boxplots): Rejected for semantic mismatch and coupling to
  term selector. See Context section above.
- **WebSocket streaming**: Over-engineered for daily-granularity data that changes
  at most once per day.
- **GraphQL sub-query**: Not consistent with the REST API patterns used across the
  Koku ecosystem.
