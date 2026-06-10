# ADR-0128: Unify GORM and pgxpool via stdlib.OpenDBFromPool

## Status

Accepted

## Context

Dual pools exhausted PostgreSQL max_connections with no unified config.

## Decision

Single pgxpool.Pool; GORM opens via `stdlib.OpenDBFromPool` with zero max-conns (delegated to pool).

## Consequences

One pool to configure. GORM respects pgxpool limits. Metrics from single source.

## References

- [internal/db/db.go](internal/db/db.go)
