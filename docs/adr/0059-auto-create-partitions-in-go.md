# ADR-0059: Auto-create partitions at first write in Go, not pg_partman

## Status

Accepted

## Context

Some environments don't have pg_partman extension available.

## Decision

Go code creates current + next month partitions at startup and during ingest.

## Consequences

No extension dependency. Must handle race conditions on concurrent creates. Startup validates partitions.

## References

- [internal/ingestion/pipeline.go](internal/ingestion/pipeline.go)
