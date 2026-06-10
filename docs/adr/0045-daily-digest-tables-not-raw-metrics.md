# ADR-0045: Use daily digest tables, not raw metrics in PostgreSQL

## Status

Accepted

## Context

Storing all hourly intervals in PG would be enormous; S3 retains source CSVs.

## Decision

Pre-aggregate into daily digest rows. Raw data stays in S3.

## Consequences

Small PG footprint. Trade-off: can't re-derive sub-daily stats from PG alone.

## References

- [migrations/000025+](migrations/000025+)
- [internal/ingestion/digest.go](internal/ingestion/digest.go)
