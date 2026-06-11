# ADR-0016: Use cost-engine consolidation on any underutilization; performance only at 2× headroom

## Status

Accepted

## Context

Performance-engine shouldn't recommend consolidation unless nodes are severely underused.

## Decision

Cost engine flags consolidation at any underutilization; performance only when headroom exceeds 2× (node allocatable capacity more than double observed demand). Same threshold for both engines was rejected: cost mode would miss consolidation opportunities; performance mode would flag noisy-neighbor consolidation on mildly underused nodes.

## Alternatives Considered

### Same consolidation threshold for cost and performance
Cost engine becomes too conservative (misses savings); performance engine becomes too aggressive (recommends consolidation before workloads have safe isolation margin).

### Performance consolidation at any underutilization
Triggers consolidation alerts on nodes with moderate headroom, causing false positives when teams intentionally reserve capacity for batch jobs.

## Consequences

Fewer false-positive consolidation alerts in performance mode. Cost mode more aggressive.

## References

- [internal/engine/recommend_nodes.go](internal/engine/recommend_nodes.go)
