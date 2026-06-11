# ADR-0177: Node idle classification is separate from container logic

## Status

Accepted

## Context

Nodes and containers have different resource characteristics. Container idle uses utilization-% relative to requests. Nodes carry management agents and infrastructure pods that consume CPU even when user workload pods are absent—a node with zero pods is zombie regardless of residual CPU usage.

Applying container idle settings to nodes would misclassify platform overhead.

## Decision

`ClassifyNodeIdleState()` uses distinct thresholds:

- **Zombie:** near-zero pod count plus minimal absolute CPU P95 millicores
- **Idle:** utilization-% thresholds plus pod-count caps

Thresholds are not driven by tenant container idle configuration.

Node zombie classification emits a notification code for consolidation consideration.

## Alternatives Considered

### Shared thresholds with containers

Does not account for node management overhead and kubelet/system pod consumption.

### Pod-count-only classification

Misses resource-consuming nodes that have lost user workloads but retain system pods.

## Consequences

- Tuning container idle settings does NOT affect node `idle_state`.
- Node idle classification is simpler (no per-tenant configuration yet).
- Node consolidation recommendations build on node idle state separately from container idle.

## Related Decisions

- [ADR-0172](0172-dual-path-idle-classification.md): Container authoritative idle path.

## References

- [internal/engine/node_idle_classification.go](../../internal/engine/node_idle_classification.go)
- [migrations/000111_add_node_idle_state.up.sql](../../migrations/000111_add_node_idle_state.up.sql)
