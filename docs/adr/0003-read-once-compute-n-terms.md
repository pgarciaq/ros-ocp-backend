# ADR-0003: Use "read once, compute N terms" over per-term SQL scans

## Status

Accepted

## Context

Customers configure arbitrary term windows (1–90 days). Running one SQL function per term re-scans digest rows N times.

## Decision

Single batch SELECT for max window per cluster, compute all terms in memory (decay, percentiles, margin, dual engines), batch-write results.

## Alternatives Considered

### Per-term SQL functions or stored procedures
Calling a PostgreSQL function once per configured term (1–90 days) re-scans the same digest partitions N times per cluster; with typical orgs running 7–14 terms, that multiplies I/O and planner work on every ingest cycle in `recommend_all.go`.

### Separate SELECT per term from the API layer
Issuing one query per term from Go avoids stored procedures but thrashes PostgreSQL buffer cache and connection pool under fleet-scale ingest; a single wide SELECT for the max window lets all terms share one in-memory slice.

### Materialized views per term window
Pre-computing term aggregates in materialized views would speed reads, but late-arriving operator data requires constant refresh and cannot express per-customer decay weights from `term_config.go` without exploding view count.

## Consequences

~20-30ms/cluster for all terms. Memory-bounded by max window size. Requires in-memory decay implementation.

## References

- [internal/engine/recommend_all.go](internal/engine/recommend_all.go)
- [internal/engine/term_config.go](internal/engine/term_config.go)
