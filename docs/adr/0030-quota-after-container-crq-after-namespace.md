# ADR-0030: Run quota after container recs; CRQ after namespace quota

## Status

Accepted

## Context

Quota needs persisted container recommendations; CRQ needs namespace-level quota aggregates.

## Decision

Quota plugin in Optimize phase (Phase 3); CRQ priority ordered after namespace quota.

## Consequences

Correct dependency ordering. Multi-phase execution required.

## References

- [docs/architecture/plugin-architecture.md](docs/architecture/plugin-architecture.md)
