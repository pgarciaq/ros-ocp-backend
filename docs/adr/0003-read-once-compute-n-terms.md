# ADR-0003: Use "read once, compute N terms" over per-term SQL scans

## Status

Accepted

## Context

Customers configure arbitrary term windows (1–90 days). Running one SQL function per term re-scans digest rows N times.

## Decision

Single batch SELECT for max window per cluster, compute all terms in memory (decay, percentiles, margin, dual engines), batch-write results.

## Consequences

~20-30ms/cluster for all terms. Memory-bounded by max window size. Requires in-memory decay implementation.

## References

- [internal/engine/recommend_all.go](internal/engine/recommend_all.go)
- [internal/engine/term_config.go](internal/engine/term_config.go)
