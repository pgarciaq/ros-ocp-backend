# ADR-0061: Use dual engine rows for nodes (term, engine) PK

## Status

Accepted

## Context

Node recommendations need cost/performance engines like containers.

## Decision

Same `(term, engine)` PK pattern for node recommendations, mirroring container design.

## Consequences

Consistent dual-engine pattern across resource types. UI handles both engines uniformly.

## References

- [docs/upgrade-runbook.md](docs/upgrade-runbook.md)
