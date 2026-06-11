# ADR-0118: Invalidate cost cache on threshold settings change

## Status

Accepted

## Context

Threshold changes may affect which rates are relevant for display. Fleet and savings summary aggregates also become stale when settings or underlying recommendation rows change.

## Decision

Settings PUT/DELETE and background recalculation paths invalidate caches for the affected org:

- `costdata.InvalidateCostDataCache` — Masu effective rates (`InvalidateThresholdCache` in threshold settings)
- `fleetsummary.InvalidateOrg` — fleet and savings summary LRU caches (see [ADR-0112](0112-bounded-lru-ttl-cost-cache.md))

### Invalidate-twice pattern for async jobs

Async threshold, savings, and reship jobs use **pre-trigger** invalidation (when the job is scheduled) plus **post-completion** invalidation (after coalesced work finishes):

| Phase | Purpose |
|-------|---------|
| Pre-trigger | Blocks serving long-stale cached aggregates during recalculation; concurrent GETs miss cache and query PostgreSQL |
| Post-completion | Clears entries repopulated mid-recalculation by concurrent reads while work was in-flight |

Applied in:

- `threshold_recalc_guard.go` — pre-trigger in `TriggerThresholdRecalculationAsync`, post in coalesced loop
- `savings_recalc_guard.go` — pre-trigger in `TriggerSavingsRecalculationAsync`, post in coalesced loop
- `trigger_guard.go` (reship) — post-completion only (no pre-trigger; reship does not serve cached summaries mid-flight the same way)

**Rationale:** A single post-completion invalidation leaves a window where concurrent GET requests repopulate the cache with in-progress (stale) data. Pre-trigger invalidation ensures reads during recalculation get cache misses.

## Consequences

Fresh rates and summary aggregates after settings change. One extra Masu call and DB aggregation on the next cache miss. Double invalidation during recalc is intentional and low cost (org-scoped map delete).

## Related Decisions

- [ADR-0112](0112-bounded-lru-ttl-cost-cache.md): Fleet/savings cache scope and metrics.
- [ADR-0086](0086-single-flight-threshold-recalc.md), [ADR-0125](0125-single-flight-trailing-reship.md): Coalescing guards implementing invalidate-twice.

## References

- [internal/engine/threshold_settings.go](internal/engine/threshold_settings.go)
- [internal/fleetsummary/cache.go](internal/fleetsummary/cache.go)
