# ADR-0128: Unify GORM and pgxpool via stdlib.OpenDBFromPool

## Status

Accepted

## Context

Dual pools exhausted PostgreSQL max_connections with no unified config.

### Historical context (phase-1 dual pool)

Before unification, GORM served API queries (ORM convenience) while pgx handled high-throughput ingest batches (`pgx.Batch`, COPY protocol). Running two separate connection pools to the same PostgreSQL instance caused connection exhaustion under load in phases 2–7. Phase 1 accepted dual pools as a pragmatic delivery trade-off — GORM for reads, pgx for bulk writes — with unification planned once ingest stabilized (incorporates former ADR-0268). The dual-pool period lasted approximately six months; careful balancing of independent GORM vs pgx pool limits was required until this ADR landed.

## Decision

Single pgxpool.Pool; GORM opens via `stdlib.OpenDBFromPool` with zero max-conns (delegated to pool).

## Alternatives Considered

### Separate GORM and pgxpool with coordinated limits
Tuning two independent `max_connections` settings (one per pool) was the status quo; in practice the pools summed past PostgreSQL's cluster limit during ingest spikes because API and processor pods each opened both pools with no shared accounting.

### Migrate all queries to GORM only
Eliminating pgxpool would unify on one ORM, but bulk ingest uses pgx batch APIs (`SendBatch`, `CopyFrom`) and raw SQL paths that GORM handles poorly; rewriting hot ingest loops in GORM would add allocation overhead without simplifying connection management.

### Migrate all queries to pgxpool only
Dropping GORM would remove ORM overhead, but list/detail handlers and migration tooling already rely on GORM model tags and associations; a full rewrite risked regressions across 60+ model files for marginal pool savings.

## Consequences

One pool to configure. GORM respects pgxpool limits. Metrics from single source. Eliminates the connection-exhaustion bugs that required pool-size tuning during the dual-pool phase.

## Related Decisions

- [ADR-0240](0240-connection-pool-timeout-tuning-surface.md): Pool tuning surface.
- [ADR-0093](0093-chunked-pgx-batches-500.md): Chunked pgx batches.

## References

- [internal/db/db.go](internal/db/db.go)
