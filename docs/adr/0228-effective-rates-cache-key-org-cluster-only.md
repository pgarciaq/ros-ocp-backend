# ADR-0228: Effective rates cache key is org+cluster only (date range excluded)

## Status

Accepted

## Context

The cost data provider fetches effective rates from Koku/Masu with `start_date` and `end_date` parameters. Caching must balance freshness with API call reduction on the savings hot path.

## Decision

`HTTPCostDataProvider` caches by `orgID+clusterID` with 5-minute TTL. The date range used in the fetch request is NOT part of the cache key.

A recalculation with a different lookback window may get cached rates from a prior request until TTL expires.

## Alternatives Considered

### Include date range in key

Cache never hits — ranges always differ slightly per recalc.

### No cache

Too many Masu API calls under fleet load.

### Longer TTL

Staler rates after cost model changes.

## Consequences

- Rate changes take up to 5 minutes to propagate.
- Different lookback windows within 5 minutes share the same cached rates.
- Acceptable because rates change infrequently (only when cost model updates in Koku).
- `InvalidateOrg` flushes the cost cache on explicit events.

## Related Decisions

- [ADR-0112](0112-bounded-lru-ttl-cost-cache.md): LRU cache pattern.
- [ADR-0118](0118-invalidate-cost-cache-on-settings-change.md): Invalidation on settings change.
- [ADR-0229](0229-container-savings-effective-rates-from-namespace-aggregates.md): Effective rate derivation.

## References

- [internal/cost/http_provider.go](../../internal/cost/http_provider.go)
