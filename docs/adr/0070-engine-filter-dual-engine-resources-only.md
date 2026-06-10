# ADR-0070: Use filter[engine]=cost|performance only on dual-engine resources

## Status

Accepted

## Context

GPU doesn't use cost/performance engines (uses terms differently).

## Decision

Engine filter applies to container/node/VM/PVC/quota only. GPU uses classification-based filtering.

## Consequences

No confusing engine filter on GPU endpoints. Resource-type-aware filtering.

## References

- [docs/architecture/recommendation-engines.md](docs/architecture/recommendation-engines.md)
