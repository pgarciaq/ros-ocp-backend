# ADR-0216: Business hours pending-marker stub rows (not customer schedules)

## Status

Accepted

## Context

When business hours are enabled for a cluster, Koku must reship historical data with BH-weighted digests. The reship state needs to be tracked per cluster and survive process restarts.

## Decision

Cluster-level placeholder rows in `business_hour_schedules` with `IsPendingMarkerStub=true` track reship state. These are excluded from schedule inheritance in `LoadSchedules`.

Sentinel UUID `00000000-0000-0000-0000-000000000000` represents org defaults. Pending stubs are created when BH is enabled and cleared when reship completes.

## Alternatives Considered

### Separate reship_state table

More tables for a simple boolean per cluster.

### In-memory tracking

Lost on restart; clusters would stall in unknown state.

### Column on clusters table

Conflates BH reship state with cluster metadata.

## Consequences

- DB rows that look like schedules but are not — confusing in support queries.
- `LoadSchedules` must filter stubs explicitly.
- Pending state survives process restarts (DB-persisted).

## Related Decisions

- [ADR-0124](0124-koku-reship-ros-rebuild-bh.md): Koku reship pipeline.
- [ADR-0125](0125-single-flight-trailing-reship.md): Trailing reship coalescing.
- [ADR-0218](0218-org-level-reship-single-flight-trailing-batch-coalescing.md): Org-level trigger guard.

## References

- [internal/bhschedule/pending.go](../../internal/bhschedule/pending.go)
- [internal/bhschedule/load.go](../../internal/bhschedule/load.go)
