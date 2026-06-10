# ADR-0016: Use cost-engine consolidation on any underutilization; performance only at 2× headroom

## Status

Accepted

## Context

Performance-engine shouldn't recommend consolidation unless nodes are severely underused.

## Decision

Cost engine flags consolidation at any underutilization; performance only when headroom exceeds 2×.

## Consequences

Fewer false-positive consolidation alerts in performance mode. Cost mode more aggressive.

## References

- [internal/engine/recommend_nodes.go](internal/engine/recommend_nodes.go)
