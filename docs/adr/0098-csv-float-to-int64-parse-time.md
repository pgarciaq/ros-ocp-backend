# ADR-0098: Convert CSV floats to int64 at parse time with NaN/Inf rejection

## Status

Accepted

## Context

Storing floats in DB breaks aggregation determinism.

## Decision

Parse CSV values to int64 (millicores/KiB) at parse time; reject NaN/Inf as invalid.

## Consequences

Clean integer data in DB. Parse-time validation. Invalid rows rejected early.

## References

- [internal/ingestion/csvparser.go](internal/ingestion/csvparser.go)
