# ADR-0185: Fleet/savings summary LRU cache with RBAC-scoped keys

## Status

Accepted

## Context

Fleet and savings summary are expensive aggregations invoked on every dashboard load. Different users may have different RBAC scopes (cluster and project filtering) per [ADR-0151](0151-rbac-fail-closed-cache-60s.md). A shared unscoped cache would leak data across permission boundaries.

## Decision

Separate bounded LRU+TTL caches for fleet-summary and savings-summary:

- Default capacity 256 entries, TTL 300s
- Cache keys: `org_id` + SHA-256 hash of sorted RBAC permission map
- `InvalidateOrg()` on data-changing events (11 call sites)

Users with identical RBAC scopes share cache entries; different scopes get isolated entries.

## Alternatives Considered

### No cache

Unacceptable latency for dashboard refresh at scale.

### Redis or shared cross-pod cache

Adds infrastructure dependency and cross-pod coherence complexity.

### Global invalidation on any org change

Over-invalidates unaffected orgs and reduces hit rate.

## Consequences

- Memory trade-off: distinct RBAC scopes multiply cache entries within the LRU bound.
- Cache thrashing bounded by LRU eviction plus org-scoped invalidation ([ADR-0118](0118-invalidate-cost-cache-on-settings-change.md)).
- Fleet and savings caches invalidate independently but share invalidation triggers.

## Related Decisions

- [ADR-0112](0112-bounded-lru-ttl-cost-cache.md): Bounded LRU+TTL cost cache pattern.
- [ADR-0118](0118-invalidate-cost-cache-on-settings-change.md): Invalidation on settings change.

## References

- [internal/fleetsummary/cache.go](../../internal/fleetsummary/cache.go)
