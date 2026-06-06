# Recommendation History & Quality

!!! info "Quick Facts"
    **History API:** `GET /api/cost-management/v1/recommendations/openshift/history`  
    **Quality API:** `GET /api/cost-management/v1/recommendations/openshift/quality`  
    **Export:** `?format=csv` on both endpoints  
    **Configurable:** Retention via `ROS_HISTORY_RETENTION_DAYS` (default 90)

Container-scoped features for tracking recommendation changes over time and measuring recommendation effectiveness.

## Overview

Recommendation **history** records how sizing values change over time.
Recommendation **quality** measures whether recommendations are stable, adopted,
and free of post-recommendation OOM events. Together they help operators trust
ROS guidance and detect flip-flopping or ignored recommendations.

## History {#history}

Recommendation history is a **fleet-wide** API. There is no per-container ID sub-resource.

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

Filter to a single container (or cluster, project, workload) with query parameters:

```
GET /api/cost-management/v1/recommendations/openshift/history?filter[container]=<name>&filter[cluster]=<cluster_alias>
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

Each row is one container + `term` + `engine` snapshot at `recorded_at`. Data is retained for `ROS_HISTORY_RETENTION_DAYS` (default 90).

**Not available for:** node, PVC, VM, namespace, GPU, or quota plugins — container recommendations only.

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

### Scope limits (by design)

These are intentional boundaries, not missing implementations:

| Area | Behavior |
|------|----------|
| **Fleet history API** | Container recommendations only — no node, PVC, GPU, or VM fleet `GET .../history` endpoints |
| **PVC history** | Usage time-series on PVC detail — not recommendation snapshot history |
| **Quota / cluster quota** | `history[]` is embedded in **detail** responses (`/quota/detail`, `/cluster-quota/detail`), not a separate fleet history API |

### Future work

| Item | Status |
|------|--------|
| **GPU time-slicing / MIG recommendation history** | Not implemented — GPU recommendations have list/detail APIs but no `recommendation_history` writer |
| **PVC recommendation snapshot history** | By design, not implemented — PVC detail exposes usage time-series only; see [PVC plugin](../plugin-reference/pvc.md#history) |

## Quality {#quality}

Quality metrics measure stability, adoption, and OOM signals after recommendations are issued. **Container-only.**

Quality rows are written on each recommendation cycle to `recommendation_quality`.

| Field | Meaning | Scale |
|-------|---------|-------|
| `stability_pct` | How much the new recommendation changed vs the prior cycle | **0.0–1.0** (1.0 = unchanged) |
| `adoption_detected` | Current requests match the prior recommendation within 5% | boolean |
| `oom_events_after_rec` | OOM events in the **current ingestion batch** (not cumulative since the rec was issued; repeated non-zero values across batches indicate ongoing pressure) | integer |
| `recommendation_age_hours` | Hours since the prior recommendation | integer |
| `measured_at` | UTC date bucket for the row | timestamp |

Default `filter[engine]` is **cost** when omitted.

### What “previous generation” means

**Previous generation** is the recommendation from the **last ingestion run**
before the current one — not a calendar “yesterday” label, but the prior
`recommendation_sets` row that existed immediately before `WriteRecommendations`
overwrote it. In typical daily deployments that is effectively **today’s run
vs. yesterday’s run**: each day’s short-term / cost recommendation is compared
against the previous day’s values to measure drift and quality.

The engine reads those prior values with `ReadClusterOldRecommendations()` in
[`internal/engine/quality.go`](../../internal/engine/quality.go) **before** writing
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

Counts OOM kills occurring in the **current ingestion batch** after a
recommendation was issued. Repeated non-zero values across batches indicate
ongoing pressure. Rising OOM counts on performance-engine recommendations may
signal insufficient headroom or workload changes not yet reflected in digests.

Prometheus gauges (`ros_recommendation_stability`, `ros_recommendation_adoption_rate`, `ros_recommendation_oom_rate`) are updated after each ingestion quality write; gauge stability/adoption values use a **0–100** scale (API quality fields use **0.0–1.0**). See [Monitoring](../monitoring.md).

### Quality API

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

**Not available for:** node and other non-container plugins.

### Future work

| Item | Status |
|------|--------|
| **`data_coverage_pct`** — share of expected digest days in the analysis window | Not implemented |
| **Stale recommendation archive on cleanup** — copy rows to `recommendation_history` before deleting stale `recommendation_sets` (today `ROS_STALE_CLEANUP_DAYS` deletes stale rows without archiving) | Not implemented |
| **Per-plugin quality** (node, PVC, VM, etc.) | Not implemented |

Internal design detail: [quality-metrics design](../../docs/design/quality-metrics.md).

## Retention and cleanup

| Variable | Default | Purpose |
|----------|---------|---------|
| `ROS_HISTORY_RETENTION_DAYS` | 90 | Drop `recommendation_history` and `recommendation_quality` partitions older than this |
| `ROS_STALE_CLEANUP_DAYS` | 30 | Delete `recommendation_sets` rows marked `stale = true` older than this (not archived first) |
| `ROS_STALENESS_THRESHOLD_HOURS` | 48 | Hours without cluster report before marking recommendations stale |

The ros-processor retention ticker runs every 24 hours. See [Configuration — Retention](../configuration.md#retention-and-data-lifecycle).

### Confidence on recommendations and history

`confidence_level` is a **float from 0.0 to 1.0** (1.0 = highest confidence) on live container recommendations and on container history rows. It is **not** exposed on `/quality` list rows.

When `confidence_level` is below `low_confidence_threshold` (configurable via container settings; default **0.5**) and digest data exists (`data_days > 0`), notification code **1** (`NotifLowConfidence`) is emitted. See [Notification codes — Containers](../architecture/notification-codes.md#containers).

### Pipeline resilience

History and quality writes are **non-fatal**: if the analytics pipeline fails (database error, timeout), container recommendations are still persisted successfully.

- **Containers:** Failed `WriteRecommendationHistory` or `WriteRecommendationQuality` calls log an error, set `pipelineDegraded`, and processing continues — recommendations are not blocked by analytics failures.
- **Namespaces:** Transient database errors return a Kafka retry so history is written on redelivery; permanent errors skip history but keep recommendations available.

This design ensures recommendations are never blocked by analytics failures.

## Related

- [Container recommendations](../../docs/features/container-recommendations.md) — Source recommendations
- [Configurability](../architecture/configurability.md) — Tuning that affects stability
- [UI Integration Guide — History](../ui-integration-guide.md#13-recommendation-history) and [Quality](../ui-integration-guide.md#14-recommendation-quality)
