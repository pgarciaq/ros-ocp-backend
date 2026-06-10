# ADR-0112: Use bounded LRU+TTL cost cache (max 1000 entries)

## Status

Accepted

## Context

Unbounded sync.Map grew without limit on multi-tenant processors.

## Decision

LRU cache with configurable max entries (default 1000) and TTL.

## Consequences

Bounded memory. Eviction on cold tenants. Metrics for cache size and evictions.

## References

- [internal/costdata/lru_cache.go](internal/costdata/lru_cache.go)
