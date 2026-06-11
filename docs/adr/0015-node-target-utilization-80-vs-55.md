# ADR-0015: Use node target utilization 80% (cost) vs 55% (performance)

## Status

Accepted

## Context

Node consolidation aggressiveness differs by optimization goal.

## Decision

Cost engine targets 80% utilization (aggressive consolidation); performance targets 55% (conservative). Eighty percent leaves ~20% burst headroom—enough for rolling updates and short spikes without the 45% idle capacity implied by Kruize's 55% cost target. Ninety-five percent was rejected because it leaves almost no room for node failure or traffic bursts.

## Alternatives Considered

### 55% target from Kruize (both engines)
Treating Kruize's conservative 55% as the cost-engine target over-provisions infrastructure by ~45% relative to actual usage, undermining consolidation savings.

### 95% cost target
Maximizes density but removes burst capacity; clusters saw scheduling pressure during deploys and node drains in validation runs.

## Consequences

Clear cost/performance trade-off for infrastructure teams. Different node counts per engine.

## References

- [internal/engine/recommend_nodes.go](internal/engine/recommend_nodes.go)
