# ADR-0176: Namespace idle aggregation rules

## Status

Accepted

## Context

Namespace-level idle state must be derived from container states ([ADR-0014](0014-namespace-idle-after-container-gpu-priority-90.md)). Ambiguous rules—majority vote or "most severe wins"—produce misleading namespace flags when workloads are mixed.

## Decision

`AggregateNamespaceIdleState()` applies these rules after container recommendations (plugin priority 90):

- **Zombie:** only if EVERY container in the namespace is zombie
- **Idle:** all containers are non-active with at least one idle (idle + zombie mix → idle)
- **Active:** otherwise

A single active container keeps the namespace active.

## Alternatives Considered

### Majority vote

Misleading when a minority of active containers represents real workload.

### Most severe (zombie overrides idle)

Incorrectly flags namespaces with partial disuse as fully zombie.

## Consequences

- Partial zombie + idle is reported as idle (more conservative; zombie implies complete disuse).
- Namespace `idle_state` changes when containers are added or removed.
- Node and container idle logic remain separate ([ADR-0177](0177-node-idle-separate-from-container.md)).

## Related Decisions

- [ADR-0014](0014-namespace-idle-after-container-gpu-priority-90.md): Namespace aggregation timing and priority.
- [ADR-0172](0172-dual-path-idle-classification.md): Container idle classification.
- [ADR-0177](0177-node-idle-separate-from-container.md): Node idle is independent.

## References

- [internal/engine/idle_classification.go](../../internal/engine/idle_classification.go) — `AggregateNamespaceIdleState`
