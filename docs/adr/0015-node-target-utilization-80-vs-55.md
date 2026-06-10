# ADR-0015: Use node target utilization 80% (cost) vs 55% (performance)

## Status

Accepted

## Context

Node consolidation aggressiveness differs by optimization goal.

## Decision

Cost engine targets 80% utilization (aggressive consolidation); performance targets 55% (conservative).

## Consequences

Clear cost/performance trade-off for infrastructure teams. Different node counts per engine.

## References

- [internal/engine/recommend_nodes.go](internal/engine/recommend_nodes.go)
