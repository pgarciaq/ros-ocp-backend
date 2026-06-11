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

## References

- [internal/costdata/lru_cache.go](internal/costdata/lru_cache.go)
