# ADR-0112: Use bounded LRU+TTL cost cache (max 1000 entries)

## Status

Accepted

## Context

Unbounded sync.Map grew without limit on multi-tenant processors.

## Decision

LRU cache with configurable max entries (default 1000) and TTL.

## Alternatives Considered

### Redis/Valkey shared cache
A distributed cache would share effective_rates across API and processor pods and survive restarts, but Masu rates change infrequently per org and adding Redis dependency to ROS doubles operational surface in on-prem charts where Valkey is already contended by Koku workers.

### Unbounded sync.Map (status quo)
The prior `sync.Map` never evicted entries; long-running processor pods on multi-tenant SaaS accumulated one entry per `(org_id, cluster)` indefinitely, producing OOM kills during fleet-wide ingest before bounded LRU was introduced in `internal/costdata/lru_cache.go`.

### TTL-only cache without LRU eviction
Pure TTL expiration avoids LRU bookkeeping but still allows unbounded growth when many distinct org/cluster pairs are touched within the TTL window (e.g., batch re-ingest across hundreds of clusters); LRU caps worst-case memory regardless of access pattern.

## Consequences

Bounded memory. Eviction on cold tenants. Metrics for cache size and evictions.

## Fleet and Savings Summary Caches

The same LRU+TTL pattern is applied to fleet summary and savings summary API responses in `internal/fleetsummary/cache.go`.

- **Per-process scope** — no Redis/Valkey; each API pod maintains its own cache. Adversarial review v5.0 accepted this trade-off for operational simplicity (see Alternatives Considered above).
- **Env vars:** `ROS_FLEET_SUMMARY_CACHE_TTL` (default 300s), `ROS_FLEET_SUMMARY_CACHE_CAPACITY` (default 256). Savings summary shares TTL/capacity settings.
- **Dual LRU instances** — one for fleet summary, one for savings summary (default rollup only; `group_by` variants are not cached).
- **Cache keys** — keyed by `org_id` plus RBAC permission hash so scoped responses do not leak across users.

## RBAC-Scoped Cache Key Construction

`CacheKey()` and `SavingsCacheKey()` in `internal/fleetsummary/cache.go` prevent cross-user cache leakage:

- Unscoped requests: `{org_id}:all`
- RBAC-scoped requests: `{org_id}:rbac:{sha256_prefix}` where the hash is computed over a JSON serialization of the sorted RBAC permissions map
- `SavingsCacheKey()` appends `:savings:{engineProfile}:{termProfile}` to the base key

Users with different RBAC scopes never share cached fleet or savings summary results even within the same org.

## Invalidation Contract

`fleetsummary.InvalidateOrg(orgID)` drops both fleet and savings entries for one org (not a global flush). Eleven call-site categories:

1. Ingest completion
2. Settings PUT/DELETE (business hours)
3. Settings PUT/DELETE (quotas and other threshold types)
4. Retention sweep
5. Sources cleanup
6. Threshold recalc completion
7. Savings recalc completion
8. Reship completion
9. Cost model changes
10. Tag sync
11. Startup

Pre-trigger invalidation on async recalc scheduling and post-completion invalidation in coalescing guard loops implement the invalidate-twice pattern ([ADR-0118](0118-invalidate-cost-cache-on-settings-change.md)).

Invalidation is org-scoped; coalescing guards limit burst invalidations during rapid admin edits.

## Prometheus Metrics

Fleet summary: `rosocp_fleet_summary_cache_{size,hits,misses,removals,invalidations}_total` (size is a gauge; others are counters).

Savings summary: parallel `rosocp_savings_summary_cache_*` metrics with the same suffixes.

Cost data cache (`internal/costdata`) exposes its own hit/miss/eviction metrics; settings changes also call `costdata.InvalidateCostDataCache`.

## Related Decisions

- [ADR-0118](0118-invalidate-cost-cache-on-settings-change.md): Invalidation triggers on settings and async recalc.
- [ADR-0086](0086-single-flight-threshold-recalc.md), [ADR-0125](0125-single-flight-trailing-reship.md): Coalescing guards that trigger post-completion invalidation.

## References

- [internal/costdata/lru_cache.go](internal/costdata/lru_cache.go)
- [internal/fleetsummary/cache.go](internal/fleetsummary/cache.go)
