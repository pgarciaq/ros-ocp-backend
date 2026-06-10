# ADR-0093: Use chunked pgx batches (max 500 queued)

## Status

Accepted

## Context

Single giant batch transactions consume excessive memory.

## Decision

Cap pgx batch queue at 500 statements; flush and continue.

## Consequences

Bounded memory per batch. More round-trips. Acceptable for ingest throughput.

## References

- [internal/ingestion/pipeline.go](internal/ingestion/pipeline.go)
