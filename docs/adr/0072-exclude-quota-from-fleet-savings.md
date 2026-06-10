# ADR-0072: Exclude quota/CRQ from fleet savings to avoid double-count

## Status

Accepted

## Context

Namespace capacity freed overlaps with container deltas (same resources).

## Decision

Don't sum quota savings into fleet total.

## Consequences

No double-counting. Quota savings visible per-recommendation only.

## References

- [docs/architecture/cost-integration.md](docs/architecture/cost-integration.md)
