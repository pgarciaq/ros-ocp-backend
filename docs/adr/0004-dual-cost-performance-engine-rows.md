# ADR-0004: Use dual cost/performance engine rows per term

## Status

Accepted

## Context

UI and FinOps users need both conservative (cost) and headroom (performance) sizing from the same telemetry.

## Decision

Every term-based plugin emits two rows per entity: `engine=cost` and `engine=performance` with different percentile/target configurations.

## Consequences

Doubles recommendation row count. Enables single-ingest dual-perspective UX. PK includes `(term, engine)`.

## References

- [docs/architecture/recommendation-engines.md](docs/architecture/recommendation-engines.md)
- [internal/engine/types.go](internal/engine/types.go)
