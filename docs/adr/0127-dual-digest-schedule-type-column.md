# ADR-0127: Store dual digest streams (schedule_type=all_hours|business_hours)

## Status

Accepted

## Context

Separate BH tables would duplicate schema and migration complexity.

## Decision

Same digest tables with `schedule_type` discriminator column.

## Consequences

No schema duplication. Queries filter on schedule_type. Slightly larger tables.

## References

- [internal/ingestion/pipeline_business_hours.go](internal/ingestion/pipeline_business_hours.go)
