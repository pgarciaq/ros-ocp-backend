# ADR-0058: Partition usage/history/quality by usage_start / month

## Status

Accepted

## Context

Need efficient retention (DROP PARTITION) without scanning and deleting individual rows.

## Decision

Monthly RANGE partitions on `usage_start` or `bucket_date`.

## Alternatives Considered

### Row-level DELETE for retention
Deleting rows older than N months with `DELETE FROM ... WHERE usage_start < $1` triggers massive autovacuum churn, table bloat, and long-running locks on digest and history tables that remain hot during ingest—retention jobs exceeded `statement_timeout` in staging.

### pg_partman-managed partitions only
The `pg_partman` extension automates partition lifecycle, but cost-onprem ships plain PostgreSQL 16 without guaranteed extension availability; relying on pg_partman would fail chart installs that don't include contrib extensions.

### TimescaleDB hypertables with drop_chunk retention
TimescaleDB's `drop_chunks()` gives O(1) retention, but the extension is unavailable on-prem and on RDS; native PostgreSQL `PARTITION BY RANGE (usage_start)` achieves the same drop semantics with zero extension dependency in `retention.go`.

## Consequences

O(1) retention via partition drop. Requires partition pre-creation. Standard PostgreSQL pattern.

## References

- [internal/engine/retention.go](internal/engine/retention.go)
