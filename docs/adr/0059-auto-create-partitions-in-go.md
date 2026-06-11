# ADR-0059: Auto-create partitions at first write in Go, not pg_partman

## Status

Accepted

## Context

Some environments don't have pg_partman extension available.

## Decision

Go code creates current + next month partitions at startup and during ingest.

## Alternatives Considered

### pg_partman extension for automatic partition creation
Delegating partition DDL to pg_partman removes application code, but the extension is not bundled in cost-onprem PostgreSQL images and cannot be assumed in SaaS RDS—partition creation would fail silently on fresh installs.

### Startup-only partition creation
Creating partitions once at process start covers steady-state months, but late-arriving operator data for prior or future months (clock skew, backfill manifests) hits "no partition for row" errors unless ingest also ensures the target month exists in `pipeline.go`.

### Manual operator intervention (runbook DDL)
Documenting `CREATE TABLE ... PARTITION OF` steps for SREs avoids code complexity, but on-prem customers lack DBA staffing; an ingest-time auto-create with idempotent `CREATE TABLE IF NOT EXISTS` keeps the pipeline self-healing.

## Consequences

No extension dependency. Must handle race conditions on concurrent creates. Startup validates partitions.

## References

- [internal/ingestion/pipeline.go](internal/ingestion/pipeline.go)
