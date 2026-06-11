# ADR-0060: Separate recommendation_history from live recommendation_sets

## Status

Accepted

## Context

Overwriting history on each ingest prevents quality/stability metrics over time.

## Decision

Maintain separate `recommendation_history` table preserving all past states.

## Alternatives Considered

### PostgreSQL temporal tables (system-period versioning)
Built-in temporal tables (`PERIOD FOR SYSTEM_TIME`) would version rows automatically, but require the `temporal_tables` extension or PG 17+ features not available in cost-onprem PG 16; a dedicated history table works on stock PostgreSQL.

### Append-only rows in the same recommendation_sets table
Keeping all historical states in the live table avoids a second table, but list queries would need `DISTINCT ON` or window functions over unbounded history, slowing `org_container_keys` sync and complicating the `(term, engine)` primary key on every ingest overwrite.

### Full event sourcing with immutable event log
An event-sourced model would capture every state transition, but replay logic, snapshot compaction, and projection rebuilds add complexity disproportionate to the quality/adoption metrics computed in `history.go`.

## Consequences

Enables quality metrics, adoption tracking, stability scoring. More storage.

## References

- [internal/engine/history.go](internal/engine/history.go)
