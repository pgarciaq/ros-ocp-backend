# Namespace Recommendations (internal reference)

The **namespace** plugin aggregates container usage digests per namespace and
produces CPU/memory request and limit targets (cost and performance engines,
short/medium/long terms). It complements the **quota** plugin, which right-sizes
existing ResourceQuota hard limits.

## API

- **List:** `GET /api/cost-management/v1/recommendations/openshift/namespaces`
- **Detail:** `GET .../namespaces/{recommendation-id}`
- **History:** `GET .../namespaces/{recommendation-id}/history`

Public feature page: [docs-site/features/namespace-recommendations.md](../../docs-site/features/namespace-recommendations.md).

## Filters

List supports `filter[cluster]`, `filter[project]` (namespace), tag filters,
pagination, sorting, **`filter[term]`** (`short`/`medium`/`long` or
`short_term`/`medium_term`/`long_term`; legacy flat `?term=` also accepted),
**`filter[engine]`** (`cost` or `performance`; legacy flat `?engine=` also
accepted), and **`filter[stale]`**:

| Term/engine filter | Behavior |
|--------------------|----------|
| omitted | List items include `short_term` cost only (slim default) |
| `filter[term]=medium_term` | Only the medium-term block under `recommendation_terms` |
| `filter[engine]=performance` | Only the performance engine under each included term |

| Engine filter | Behavior |
|---------------|----------|
| omitted | Both `cost` and `performance` under each `recommendation_terms.<term>.recommendation_engines` |
| `filter[engine]=cost` | Only the cost engine block is returned |
| `filter[engine]=performance` | Only the performance engine block is returned |

List items use a slim list DTO
([`BuildNamespaceListResponse`](../../internal/model/list_response.go)) that
preserves table columns (`current`, selected term cost `variation`,
`notification_codes`, `monitoring_end_time`) while omitting plots and duplicate
notification nesting. Unfiltered list still returns `NamespaceDetailResponse`
rows (ADR-0294 fat default) but **omits** nested `business_hours` ([#497](https://github.com/pgarciaq/ros-ocp-backend/issues/497)).
Detail still uses
[`BuildNamespaceDetailResponse`](../../internal/model/detail_response.go)
and keeps the nest. Rec persist is all-hours only
([#516](https://github.com/pgarciaq/ros-ocp-backend/issues/516)); BH nest is GET-time
from `daily_namespace_digests`.

Cost uses lower usage percentiles for rightsizing; performance uses higher
percentiles for headroom (same model as container recommendations).

Utilization charts share the container Peak hours second-chart pattern
([#496](https://github.com/pgarciaq/ros-ocp-backend/issues/496)): all-hours
stay 24×7 `plots` + 24×7 rec; Peak hours uses `business_hours_plots` + BH
request/limit. Hide Peak hours when the nest is reason-only or BH plots are
empty.

| Value | Behavior |
|-------|----------|
| `false` | Default — exclude stale rows |
| `true` | Include stale and non-stale |
| `only` | Only stale rows |

Legacy alias: `?stale=` (same values).

## Staleness

When a cluster has not reported within **`ROS_STALENESS_THRESHOLD_HOURS`** (default **48**),
namespace rows are marked `stale = true` and notification code **2** (`STALE_DATA`) is
attached. Fresh ingestion clears staleness on the next recommendation run.

Full behavior, API semantics, and lifecycle: [docs/operations/stale-detection.md](../operations/stale-detection.md).
