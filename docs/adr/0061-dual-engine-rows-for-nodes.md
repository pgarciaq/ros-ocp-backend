# ADR-0061: Use dual engine rows for nodes (term, engine) PK

## Status

Accepted

## Context

Node recommendations need cost/performance engines like containers.

## Decision

Same `(term, engine)` PK pattern for node recommendations, mirroring container design.

## Related Decisions

- [ADR-0004](0004-dual-cost-performance-engine-rows.md): container dual rows established the `(term, engine)` pattern this ADR extends to nodes.
- [ADR-0078](0078-nested-node-list-medium-term-cost.md): nested node list aggregates both engine rows under one API object.

## Consequences

Consistent dual-engine pattern across resource types. UI handles both engines uniformly.

## References

- [docs/upgrade-runbook.md](docs/upgrade-runbook.md)
