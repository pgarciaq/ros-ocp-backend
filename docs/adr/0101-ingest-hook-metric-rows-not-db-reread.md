# ADR-0101: Pass []MetricRow to IngestHook, not DB re-read

## Status

Accepted

## Context

Post-write SELECT adds I/O and coupling to digest schema.

## Decision

IngestHook receives in-memory `[]MetricRow` from the same parse pass (Option B).

## Consequences

Zero extra I/O. Hook sees same data as primary ingest. Memory cost of holding rows during hooks.

## References

- [docs/architecture/plugin-architecture.md](docs/architecture/plugin-architecture.md)
