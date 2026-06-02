# Recommendation History & Quality

!!! info "Quick Facts"
    **History API:** `GET /api/cost-management/v1/recommendations/openshift/history`  
    **Quality API:** `GET /api/cost-management/v1/recommendations/openshift/quality`  
    **Export:** `?format=csv` on both endpoints  
    **Configurable:** Retention via `ROS_HISTORY_RETENTION_DAYS` (default 90)

## Overview

Recommendation **history** records how sizing values change over time.
Recommendation **quality** measures whether recommendations are stable, adopted,
and free of post-recommendation OOM events. Together they help operators trust
ROS guidance and detect flip-flopping or ignored recommendations.

## History

Each time recommendations are generated, prior values are archived to
`recommendation_history` with a `recorded_at` timestamp. History captures CPU
and memory request/limit values per container × term × engine.

### Use cases

- **Trend analysis** — See whether recommendations converge or oscillate
- **Audit** — Prove what ROS suggested before a capacity incident
- **Adoption tracking** — Compare history to current cluster config (see quality)

### API

```http
GET /api/cost-management/v1/recommendations/openshift/history
GET /api/cost-management/v1/recommendations/openshift/history?format=csv
```

| Filter | Parameter |
|--------|-----------|
| Cluster | `cluster` (UUID or alias) |
| Project | `project` |
| Workload | `workload` |
| Container | `container` |
| Term | `term` (`short`, `medium`, `long`) |
| Engine | `engine` (`cost`, `performance`) |
| Date range | `start_date`, `end_date` (YYYY-MM-DD; default: current month) |

Sort by `recorded_at`, `cluster`, `project`, `container`, `term`, or `engine`.
Pagination via `offset` and `limit` (offset-only by design — see [API Pagination](../pagination.md)).

### Example (abbreviated)

```json
{
  "meta": { "count": 150 },
  "data": [{
    "recorded_at": "2026-05-20T08:00:00Z",
    "cluster_alias": "prod-east",
    "project": "payments",
    "container": "api",
    "term": "medium",
    "engine": "cost",
    "cpu_request_mc": 500,
    "memory_request_kib": 524288,
    "cpu_limit_mc": 525,
    "memory_limit_kib": 550502
  }]
}
```

## Quality metrics

Quality rows are written on each recommendation cycle to `recommendation_quality`.

| Metric | Meaning |
|--------|---------|
| **stability_pct** | How much CPU/memory recommendations changed vs the **previous generation** (1.0 = unchanged) — see below |
| **adoption_detected** | Current cluster config matches prior recommendation within 5% tolerance |
| **oom_events_after_rec** | OOM kills since the recommendation was issued |
| **recommendation_age_hours** | Hours since the recommendation was last updated |

### What “previous generation” means

**Previous generation** is the recommendation from the **last ingestion run**
before the current one — not a calendar “yesterday” label, but the prior
`recommendation_sets` row that existed immediately before `WriteRecommendations`
overwrote it. In typical daily deployments that is effectively **today’s run
vs. yesterday’s run**: each day’s short-term / cost recommendation is compared
against the previous day’s values to measure drift and quality.

The engine reads those prior values with `ReadClusterOldRecommendations()` in
[`internal/engine/quality.go`](../internal/engine/quality.go) **before** writing
new rows. Archived snapshots also land in `recommendation_history` for trend
APIs, but stability and adoption metrics use the pre-overwrite `recommendation_sets`
comparison.

### Stability

Stability scores how much recommended CPU and memory changed compared to that
previous generation:

```
stability = max(0, 1.0 − |cpu_variation|/100 × 0.5 − |mem_variation|/100 × 0.5)
```

A score of **1.0** means no change since the last run; lower scores indicate
larger shifts. Use stability to detect **flip-flopping** recommendations that
may erode operator trust. Variation within ~10% on both resources yields high
stability (~0.9+).

### Adoption

Adoption is detected when the workload's **current** CPU and memory requests
match the **previous generation** recommendation (same prior-run snapshot as
stability) within **5% tolerance** on both dimensions. Useful for verifying
that teams applied ROS guidance before the latest recalculation.

### OOM events

Counts OOM kills occurring after a recommendation was issued. Rising OOM counts
on performance-engine recommendations may signal insufficient headroom or
workload changes not yet reflected in digests.

## Quality API

```http
GET /api/cost-management/v1/recommendations/openshift/quality
GET /api/cost-management/v1/recommendations/openshift/quality?format=csv
```

| Filter | Parameter |
|--------|-----------|
| Cluster | `cluster` |
| Project | `project` |
| Workload | `workload` |
| Container | `container` |
| Engine | `filter[engine]` or `engine` (`cost`, `performance`; defaults to `cost`) |
| Date range | `start_date`, `end_date` |

Sort by `measured_at`, `stability`, `adoption`, `oom_events`, or
`recommendation_age`.

### Example (abbreviated)

```json
{
  "data": [{
    "measured_at": "2026-05-20T08:00:00Z",
    "container": "api",
    "project": "payments",
    "engine": "cost",
    "stability_pct": 0.95,
    "adoption_detected": true,
    "oom_events_after_rec": 0,
    "recommendation_age_hours": 168
  }]
}
```

## Retention

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_HISTORY_RETENTION_DAYS` | 90 | Purge history records older than N days |

Quality rows follow the same retention policy as part of the recommendation lifecycle.

## Related

- [Container Right-Sizing](container-recommendations.md) — Source recommendations
- [Configurable Thresholds](configurable-thresholds.md) — Tuning that affects stability
- [UI Integration Guide — Appendix](../ui-integration-guide.md#appendix-additional-native-endpoints)
