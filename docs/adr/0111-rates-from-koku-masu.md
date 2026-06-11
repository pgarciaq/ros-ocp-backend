# ADR-0111: Source all rates from Koku Masu effective_rates

## Status

Accepted

## Context

Duplicating cost model configuration in ROS would diverge from Koku source of truth.

## Decision

ROS fetches rates from Koku's internal Masu endpoint. No rate CRUD in ROS.

## Alternatives Considered

### Local cost-model CRUD in ROS
Duplicating Koku's cost model schema and rate application logic in ROS would decouple availability, but rates would diverge whenever customers update models via the Koku API—savings numbers would disagree with the Cost Management UI built on Koku's source of truth.

### Periodic snapshot of rates to a local DB table
Caching Masu rates locally on a cron would reduce runtime dependency, but stale snapshots after cost model edits produce wrong dollar savings until the next sync; the bounded LRU cache in ADR-0112 handles hot-path latency without permanent staleness.

### Hardcoded default rates with optional override
Static fallback rates simplify offline operation, but ignore customer-specific infrastructure/supplementary tiers, tag-based rates, and markup percentages that vary per org in `effective_rates`.

## Consequences

Single source of truth. Dependency on Koku availability. Kill-switch if unavailable.

## References

- [internal/costdata/provider.go](internal/costdata/provider.go)
