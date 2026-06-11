# ADR-0002: Use exact Go percentiles over TimescaleDB/t-digest

## Status

Accepted

## Context

Original design assumed TimescaleDB continuous aggregates and `tvondra/tdigest`. AWS RDS doesn't support TimescaleDB; t-digest percentiles are approximate and can't be merged correctly on late-arriving data.

## Decision

Drop TimescaleDB entirely. Compute exact percentiles in Go (`slices.Sort()` on ~96 hourly samples/day), upsert daily digest rows into `PARTITION BY RANGE` tables.

## Alternatives Considered

### TimescaleDB continuous aggregates
The original design assumed `timescaledb_toolkit` percentile aggregates refreshed hourly, but the TimescaleDB extension is unavailable on cost-onprem PostgreSQL (plain PG 16) and unsupported on AWS RDS—adding an extension dependency would block on-prem and SaaS deployments alike.

### PostgreSQL `percentile_cont` at query time
Computing percentiles with `percentile_cont()` over raw hourly rows in SQL is exact, but requires a full scan of all samples for every recommendation run; with 90-day windows and thousands of containers per cluster, that pushes work to read-heavy API paths instead of bounded ingest-time aggregation in `internal/ingestion/digest.go`.

### t-digest approximate sketches
The `tvondra/tdigest` extension offers compact mergeable summaries, but approximate percentiles diverge on small clusters (few dozen samples) and cannot be merged correctly when late-arriving CSV rows amend prior days—exact sort on ~96 hourly values per day is cheap and deterministic.

## Consequences

No extension dependencies. Exact math. Slightly more CPU at ingest (negligible at 96 samples). Raw metrics stay in S3 CSVs, not PG.

## References

- [docs/archive/requirements.md](docs/archive/requirements.md)
- [internal/ingestion/digest.go](internal/ingestion/digest.go)
