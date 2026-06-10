# ADR-0085: Use per-org threshold cache TTL 60s with async recalc on PUT

## Status

Accepted

## Context

Synchronous full-cluster recompute would block Settings API response.

## Decision

Cache thresholds 60s; PUT invalidates cache and triggers async recalculation.

## Consequences

Fast settings response. Eventually consistent recommendations (within 60s).

## References

- [internal/engine/threshold_settings.go](internal/engine/threshold_settings.go)
