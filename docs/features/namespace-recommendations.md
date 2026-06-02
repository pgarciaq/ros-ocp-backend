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
pagination, sorting, **`filter[engine]`** (`cost` or `performance`; legacy flat
`?engine=` also accepted), and **`filter[stale]`**:

| Engine filter | Behavior |
|---------------|----------|
| omitted | Both `cost` and `performance` under each `recommendation_terms.<term>.recommendation_engines` |
| `filter[engine]=cost` | Only the cost engine block is returned |
| `filter[engine]=performance` | Only the performance engine block is returned |

Cost uses lower usage percentiles for rightsizing; performance uses higher
percentiles for headroom (same model as container recommendations).

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
