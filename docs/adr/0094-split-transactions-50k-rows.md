# ADR-0094: Use split transactions above 50k rows per phase

## Status

Accepted

## Context

One multi-hour transaction holds locks and risks timeout.

## Decision

Split into sub-transactions at 50k row boundaries within each ingest phase.

## Consequences

Shorter lock hold times. Partial progress on failure. Must handle idempotent retries.

## References

- [internal/ingestion/pipeline.go](internal/ingestion/pipeline.go)
