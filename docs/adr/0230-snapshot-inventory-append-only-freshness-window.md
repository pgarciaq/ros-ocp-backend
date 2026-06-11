# ADR-0230: Snapshot inventory append-only with freshness window for classification

## Status

Accepted

## Context

Snapshot recommendations need current inventory state (which snapshots exist, sizes, labels). Inventory data arrives periodically from the operator. Classification must use recent data without losing audit history.

## Decision

`snapshot_inventory` is append-only (new rows on each report). Classification reads `DISTINCT ON ... WHERE ingested_at >= now - inventory_fresh_hours` for latest state per snapshot.

Raw rows purged separately (`SnapshotInventoryRetentionH`, default 48h). Stale inventory (no fresh rows within window) → classification skipped, not errored.

## Alternatives Considered

### Upsert/replace

Loses audit trail of inventory changes over time.

### Separate "current" table

Additional migration and sync logic between append and current views.

### Never purge

Unbounded table growth.

## Consequences

- Table grows between purge cycles.
- Classification depends on fresh data — if operator stops reporting, snapshot recs go stale.
- Purge and classification use different time windows (purge may be longer than freshness).

## Related Decisions

- [ADR-0031](0031-snapshot-priority-ordered-rules.md): Snapshot priority rules.
- [ADR-0032](0032-snapshot-restoresize-for-cost.md): RestoreSize for cost.
- [ADR-0132](0132-retention-policies-per-table.md): Retention policies.

## References

- [internal/plugins/snapshot/inventory.go](../../internal/plugins/snapshot/inventory.go)
- [internal/plugins/snapshot/classify.go](../../internal/plugins/snapshot/classify.go)
