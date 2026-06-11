# ADR-0268: Phase-1 dual pool (pgx + GORM) before unification

## Status

Accepted

## Phase

1

## Context

GORM was used for API queries (convenient ORM, familiar patterns). pgx was introduced for high-throughput ingest batches (pgx.Batch, COPY protocol). Running two separate connection pools to the same PostgreSQL instance caused connection exhaustion under load in phases 2–7.

Immediate unification would have delayed phase-1 delivery.

## Decision

Phase 1 accepted dual pools as a pragmatic choice — GORM for reads, pgx for bulk writes. Phase 8 unified via `stdlib.OpenDBFromPool` ([ADR-0128](0128-unify-gorm-pgxpool-stdlib.md)). The dual-pool phase lasted approximately six months.

## Alternatives Considered

### GORM-only

Too slow for bulk ingest batches (500+ row upserts).

### pgx-only from start

Loses ORM convenience for complex API list queries.

### Immediate unification in phase 1

Would have delayed phase-1 delivery timeline.

## Consequences

- Connection exhaustion bugs in phases 2–7 required pool size tuning.
- Careful balancing of GORM vs pgx pool limits needed.
- Unification in ADR-0128 resolved the issue permanently.
- Technical debt documented for future teams.

## Related Decisions

- [ADR-0128](0128-unify-gorm-pgxpool-stdlib.md): Unify GORM and pgxpool.
- [ADR-0240](0240-connection-pool-timeout-tuning-surface.md): Pool tuning surface.
- [ADR-0093](0093-chunked-pgx-batches-500.md): Chunked pgx batches.

## References

- [internal/db/pool.go](../../internal/db/pool.go)
- [internal/db/gorm.go](../../internal/db/gorm.go)
