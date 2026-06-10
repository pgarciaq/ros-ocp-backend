# ADR-0002: Use exact Go percentiles over TimescaleDB/t-digest

## Status

Accepted

## Context

Original design assumed TimescaleDB continuous aggregates and `tvondra/tdigest`. AWS RDS doesn't support TimescaleDB; t-digest percentiles are approximate and can't be merged correctly on late-arriving data.

## Decision

Drop TimescaleDB entirely. Compute exact percentiles in Go (`slices.Sort()` on ~96 hourly samples/day), upsert daily digest rows into `PARTITION BY RANGE` tables.

## Consequences

No extension dependencies. Exact math. Slightly more CPU at ingest (negligible at 96 samples). Raw metrics stay in S3 CSVs, not PG.

## References

- [docs/architecture/requirements.md](docs/architecture/requirements.md)
- [internal/ingestion/digest.go](internal/ingestion/digest.go)
