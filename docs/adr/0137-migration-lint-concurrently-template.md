# ADR-0137: Use migration lint + CONCURRENTLY job template for large-table indexes

## Status

Accepted

## Context

Blocking CREATE INDEX on production tables causes write locks.

## Decision

CI lint script flags non-CONCURRENTLY indexes; Helm job template for manual CONCURRENTLY builds.

## Consequences

No accidental blocking indexes in production. Manual step for large tables.

## References

- [scripts/lint-migrations.sh](scripts/lint-migrations.sh)
- [deploy/migrations/concurrent-index-job.yaml](deploy/migrations/concurrent-index-job.yaml)
