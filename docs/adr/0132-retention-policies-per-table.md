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

## Status Update (2026-06)

[ADR-0203](0203-retention-side-effects-beyond-partition-drop.md) documents retention side effects beyond partition drops: stale recommendation row deletion with per-org cache invalidation, historical namespace set purge, snapshot inventory cleanup (48h), and the separate 90-day window for history/quality data (longer than recommendation retention).
