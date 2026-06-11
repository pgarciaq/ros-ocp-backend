# ADR-0072: Exclude quota/CRQ from fleet savings to avoid double-count

## Status

Accepted

## Context

Namespace capacity freed overlaps with container deltas (same resources).

## Decision

Don't sum quota savings into fleet total.

## Alternatives Considered

### Include quota in fleet savings
Double-counts container savings already reflected in fleet total—the same freed CPU/memory appears in both namespace quota deltas and container right-sizing.

### Zero-out quota savings in API
Confusing: per-recommendation shows savings but fleet total silently ignores quota plugin output without explanation.

## Related Decisions

- Quota savings appear only in quota-specific list/detail views; fleet `savings-summary` aggregates container, node, PVC, and snapshot plugins only.

## Consequences

No double-counting. Quota savings visible per-recommendation only.

## References

- [docs/architecture/cost-integration.md](docs/architecture/cost-integration.md)
