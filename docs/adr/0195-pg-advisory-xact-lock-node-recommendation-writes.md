# ADR-0195: pg_advisory_xact_lock for node recommendation writes

## Status

Accepted

> **Note:** This ADR documents a straightforward or inherited decision kept for completeness and historical traceability. It does not represent a non-obvious architectural fork.

## Context

Node recommendation persistence and migration 000058 (PK rebuild) can deadlock when running concurrently. Traditional row-level locking is insufficient because the PK rebuild acquires table-level locks.

## Decision

`PersistNodeRecommendations` and node savings recalc acquire `pg_advisory_xact_lock(7358001)` within their transaction. This serializes writes with the migration without requiring worker shutdown. Lock ID **7358001** is reserved for node-recommendation operations.

The lock is transaction-scoped (auto-released on commit/rollback).

## Consequences

- Node writes are serialized (one writer at a time).
- Lock is transaction-scoped (auto-released on commit/rollback).
- Operational migrations must preserve lock ID 7358001.
- Other resource types don't need this (their PK rebuilds don't conflict with write patterns).

## Alternatives Considered

### Worker shutdown during migration

Operational complexity and ingest downtime. Rejected.

### LOCK TABLE

Too broad — blocks reads unnecessarily. Rejected.

### Row-level FOR UPDATE

Doesn't prevent PK rebuild deadlock. Rejected.

## Related Decisions

- [ADR-0045](0045-daily-digest-tables-not-raw-metrics.md): Node recommendation persistence model.

## References

- [internal/engine/recommend_nodes.go](../../internal/engine/recommend_nodes.go)
- [internal/engine/savings_recalculate.go](../../internal/engine/savings_recalculate.go)
