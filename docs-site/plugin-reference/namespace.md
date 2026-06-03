# namespace

Package: [`internal/plugins/namespace`](../../internal/plugins/namespace/)

**Namespace sizing** — aggregates container usage digests per namespace and recommends CPU/memory request and limit targets for each term × engine (`cost` / `performance`).

## Plugin metadata

| Property | Value |
|----------|-------|
| Name | `namespace` |
| Phase | 1 (Produce) |
| Priority | 90 (runs after container at 10) |
| CSV types | `namespace` (namespace ROS usage CSV) |
| Retention tables | `daily_namespace_digests`, `namespace_usage_samples` |

## Traits

| Trait | Supported |
|-------|-----------|
| CSVIngestor | Yes — `ProcessNamespaceCSVToDigests` |
| APIProvider | Yes — list, detail, history |
| TermProvider | Yes — short/medium/long (max 90 days) |

**Special behavior:** HTTP routes stay registered in Kruize mode so namespace APIs remain available; native CSV ingestion follows `plugin.Enabled` mutual exclusivity.

## What it does

1. Ingest namespace-level CPU/memory digests from the metrics operator.
2. Run `RecommendAllNamespaces` with percentile-based sizing per engine profile.
3. After container (and GPU) recommendations exist, `AggregateNamespaceIdleState` sets namespace `idle_state` from child workloads.

Dual engines on every response: `cost` (tighter percentiles) and `performance` (higher headroom). See [Dual engine](../features/dual-engine.md).

## Endpoint

```
GET /api/cost-management/v1/recommendations/openshift/namespaces
GET /api/cost-management/v1/recommendations/openshift/namespaces/{recommendation-id}
GET /api/cost-management/v1/recommendations/openshift/namespaces/{recommendation-id}/history
```

Legacy aliases: `GET .../openshift/namespace/recommendations`, `GET .../namespace/{recommendation-id}`.

Handlers: [`GetNamespaceRecommendationSetListWithFallback`](../../internal/api/handlers.go), [`GetNamespaceRecommendationHistoryWithFallback`](../../internal/api/handlers_namespace_history.go).

## Key features

### Idle state aggregation

| `idle_state` | Meaning |
|--------------|---------|
| `active` | At least one container/GPU row is active |
| `idle` | All children idle or zombie, with at least one idle |
| `zombie` | Every container and GPU row is zombie |

List filter: `filter[idle_state]=idle,zombie` (comma-separated `active`, `idle`, `zombie`).

### Staleness

`filter[stale]` — `false` (default, fresh only), `true` (include stale), `only` (stale rows only). Stale means the recommendation is older than ~48h without fresh cluster reporting (`ROS_STALENESS_THRESHOLD_HOURS`). Distinct from VolumeSnapshot staleness (snapshot plugin).

### Other list filters

| Parameter | Description |
|-----------|-------------|
| `filter[cluster]` | Cluster UUID |
| `filter[project]` / `namespace` | Namespace name |
| `filter[tag:<key>]` | Label filter when `ROS_TAGS_ENABLED=true` |
| `order_by` / `order_how` | Sort (e.g. `namespace`, variation fields) |

## Business hours

When `ROS_BUSINESS_HOURS_ENABLED=true` and a schedule exists (org, cluster, or namespace scope), the engine persists parallel `all_hours` and `business_hours` digest streams. Detail responses include `recommendation_engines.{cost|performance}.business_hours` after reship completes.

See [Business hours](business-hours.md) and [Business Hours feature](../features/business-hours.md).

## Notification codes

Namespace rows use codes **1** (low confidence), **7** (new workload), and **9** (memory trending up). Filter: `GET /recommendations/openshift/notification-codes?filter[plugin]=namespace`.

See [Notification codes — Namespaces](../architecture/notification-codes.md#namespaces).

## Savings

Namespace recommendations provide sizing guidance only — **no dollar savings field** is included. Savings are computed at the container level and can be aggregated by namespace using `group_by[project]` on the container list or `GET /recommendations/openshift/savings-summary` (GPU still excluded from fleet totals).

## Related features

[ResourceQuota recommendations](../features/quota-recommendations.md) tune existing `ResourceQuota` **hard** limits; namespace recs propose ideal totals from observed usage.

## Architecture

- [Namespace recommendations (feature)](../features/namespace-recommendations.md)
- [Recommendation engines](../architecture/recommendation-engines.md)
- [Recommendation math](../architecture/recommendation-math.md)
- Internal design: [`docs/features/namespace-recommendations.md`](../../docs/features/namespace-recommendations.md)
