# ADR-0203: Retention side effects beyond partition drop

## Status

Accepted

## Context

[ADR-0132](0132-retention-policies-per-table.md) covers TTL-based partition drops. Retention also performs row-level cleanup and triggers system-wide side effects that operators and API consumers must understand.

## Decision

Beyond partition drops, retention performs:

1. **`DELETE FROM recommendation_sets WHERE stale=true AND updated_at < cutoff`** with fleet cache invalidation per affected `org_id` ([ADR-0112](0112-bounded-lru-ttl-cost-cache.md), [ADR-0118](0118-invalidate-cost-cache-on-settings-change.md))
2. **Historical namespace recommendation set purge**
3. **Snapshot inventory purge** (48h default)
4. **History/quality on separate 90-day window** — longer than recommendation retention

## Consequences

- Stale rec deletion is user-visible (rows disappear from list).
- Fleet cache invalidation ensures dashboard reflects deletions.
- History retention > recommendation retention means historical quality data outlives its recommendations.

## Alternatives Considered

### Partition-only retention

Leaves stale rows forever. Rejected.

### Cascading deletes

Would lose history data needed for trend analysis. Rejected.

### Soft delete

Unbounded table growth. Rejected.

## Related Decisions

- [ADR-0132](0132-retention-policies-per-table.md): TTL policies per table.
- [ADR-0112](0112-bounded-lru-ttl-cost-cache.md): Fleet cost cache.
- [ADR-0118](0118-invalidate-cost-cache-on-settings-change.md): Cache invalidation on settings change.

## References

- [internal/engine/retention.go](../../internal/engine/retention.go)
