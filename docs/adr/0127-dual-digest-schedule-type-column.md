# ADR-0127: Store dual digest streams (schedule_type=all_hours|business_hours)

## Status

Accepted

## Context

Separate BH tables would duplicate schema and migration complexity.

## Decision

Same digest tables with `schedule_type` discriminator column.

## Alternatives Considered

### Separate business-hours digest tables
Dedicated `_bh` tables would isolate schemas, but duplicate every migration, partition-management rule, and retention job in `retention.go`—any column add to digest tables requires parallel DDL on two table families.

### Re-ingest operator CSVs filtered to business hours
Parsing the same S3 files twice with a time-of-day filter avoids dual storage, but doubles ingest CPU and I/O; filtering at parse time in `pipeline_business_hours.go` produces both streams in one pass.

### Single all-hours stream with BH filter at query time
Storing only 24×7 digests and filtering samples by hour at recommendation time seems storage-efficient, but recomputing BH percentiles from hourly arrays on every engine run pushes CPU to the hot path and produces different results than pre-aggregated BH daily buckets.

## Consequences

No schema duplication. Queries filter on schedule_type. Slightly larger tables.

## References

- [internal/ingestion/pipeline_business_hours.go](internal/ingestion/pipeline_business_hours.go)
