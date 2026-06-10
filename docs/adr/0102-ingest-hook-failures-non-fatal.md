# ADR-0102: Treat IngestHook failures as non-fatal

## Status

Accepted

## Context

GPU digest upsert bug must not block container recommendations (primary product).

## Decision

Hook errors logged + counted but processing continues.

## Consequences

Container reliability isolated from auxiliary plugins. Silent degradation risk (mitigated by metrics).

## References

- [docs/architecture/plugin-architecture.md](docs/architecture/plugin-architecture.md)
