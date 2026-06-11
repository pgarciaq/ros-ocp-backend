# ADR-0101: Pass []MetricRow to IngestHook, not DB re-read

## Status

Accepted

## Context

Post-write SELECT adds I/O and coupling to digest schema.

## Decision

IngestHook receives in-memory `[]MetricRow` from the same parse pass (Option B).

## Alternatives Considered

### Re-read from DB after primary write
Extra round-trip per hook; race with concurrent writers if another ingest updates the same org between write and SELECT.

### Pass raw CSV bytes to hooks
Every hook re-parses CSV independently—duplicate CPU work and divergent parse logic if hook schema expectations drift from primary ingest.

## Consequences

Zero extra I/O. Hook sees same data as primary ingest. Memory cost of holding rows during hooks.

## References

- [docs/architecture/plugin-architecture.md](docs/architecture/plugin-architecture.md)
