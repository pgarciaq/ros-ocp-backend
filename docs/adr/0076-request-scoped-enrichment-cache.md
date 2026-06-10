# ADR-0076: Use request-scoped enrichment cache for cost rates

## Status

Accepted

## Context

Per-row Masu calls on list endpoints with many clusters would be N+1.

## Decision

Cache cost rates per-request (one fetch per cluster per request).

## Consequences

Minimal Masu calls. Fresh per-request. No stale cache across requests.

## References

- [internal/api/enrichment_cache.go](internal/api/enrichment_cache.go)
