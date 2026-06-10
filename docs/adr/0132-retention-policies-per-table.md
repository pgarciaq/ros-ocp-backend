# ADR-0132: Use retention: 6mo digests, 90d history, 30d stale recs, 48h snapshot inventory

## Status

Accepted

## Context

Indefinite accumulation of samples and stale rows wastes storage and degrades queries.

## Decision

Per-table retention policies; implemented via partition drops and batched deletes.

## Consequences

Bounded storage growth. Configurable per deployment. Historical data eventually lost.

## References

- [docs/operations/retention.md](docs/operations/retention.md)
