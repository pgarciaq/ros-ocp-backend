# ADR-0189: Pre-computed org_recommendation_stats for list counts

## Status

Accepted

## Context

`COUNT(DISTINCT …)` over `recommendation_sets` is expensive for large orgs (100k+ containers). Every list request needs total count for pagination metadata ([ADR-0066](0066-keyset-after-cursor-pagination.md)). Live counts dominate query cost on dashboard initial load.

## Decision

Maintain `org_recommendation_stats` with pre-computed counts per org, refreshed once at
the end of each reconcile cycle via `RefreshOrgMetadata` ([ADR-0289](0289-defer-org-metadata-refresh-end-of-reconcile.md)).

- List requests read counts from this table instead of computing them live
- Counts include breakdowns by stale/non-stale and plugin type
- Refresh is org-scoped, not global

## Alternatives Considered

### Live COUNT on every list request

Too slow at scale; blocks pagination metadata.

### Approximate count via EXPLAIN

Unreliable for filtered counts and poor UX when totals jump between requests.

### Client-side "load more" without total

Poor UX for paginated tables and export flows.

## Consequences

- Counts are eventually consistent—stale during active ingestion.
- Adds one small table write per ingest cycle per org.
- Eliminates the most expensive part of list queries for dashboard initial load.
- Pairs with keys-table pagination ([ADR-0188](0188-list-query-keys-pagination-refilter-detail.md)).

## Related Decisions

- [ADR-0188](0188-list-query-keys-pagination-refilter-detail.md): Keys pagination with detail re-filter.
- [ADR-0066](0066-keyset-after-cursor-pagination.md): Keyset pagination model.

## References

- [internal/model/org_recommendation_stats.go](../../internal/model/org_recommendation_stats.go)
- [migrations/000078_keyset_pagination_indexes.up.sql](../../migrations/000078_keyset_pagination_indexes.up.sql)
