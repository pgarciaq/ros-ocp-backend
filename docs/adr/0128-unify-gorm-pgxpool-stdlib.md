# ADR-0128: Unify GORM and pgxpool via stdlib.OpenDBFromPool

## Status

Accepted

## Context

Dual pools exhausted PostgreSQL max_connections with no unified config.

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

One pool to configure. GORM respects pgxpool limits. Metrics from single source.

## References

- [internal/db/db.go](internal/db/db.go)
