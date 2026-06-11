# ADR-0194: Node consolidation precedence and pod-scheduling gate

## Status

Accepted

## Context

Node recommendations can suggest fleet reduction (fewer, larger nodes). Grouping nodes for consolidation requires understanding which nodes are interchangeable. Without a pod-scheduling gate, consolidation recommendations could exceed scheduler headroom and cause scheduling failures.

## Decision

**Fleet grouping precedence:** MachineSet label > `instance_type` > rounded capacity key.

**Performance engine:** Requires 2× headroom per [ADR-0016](0016-cost-consolidation-any-performance-2x-headroom.md).

**Pod-scheduling gate:** `podSchedulingBlocksConsolidation` prevents absorbing workloads when pod headroom < `PodHeadroomConsolidationGate` (prevents over-packing).

**Notification:** Emits `NotifNodeFleetConsolidation` (code 76) on MachineSet group reductions.

Extends [ADR-0043](0043-instance-type-consolidation-level-3.md) Level-3 consolidation with MachineSet-first grouping and scheduling safety.

## Consequences

- MachineSet-based grouping is preferred (operator can act on it).
- Nodes without MachineSet fall back to type/capacity grouping.
- Pod scheduling gate prevents recommendations that would exceed scheduler limits.

## Alternatives Considered

### No pod gate

Recommendations could cause scheduling failures post-adoption. Rejected.

### Anti-affinity awareness

Requires full scheduler simulation — too complex for v1. Deferred.

## Related Decisions

- [ADR-0043](0043-instance-type-consolidation-level-3.md): Instance-type Level-3 consolidation baseline.
- [ADR-0170](0170-machineset-tier1-aggregation.md): MachineSet Tier-1 aggregation.
- [ADR-0016](0016-cost-consolidation-any-performance-2x-headroom.md): Performance engine 2× headroom.

## References

- [internal/engine/recommend_nodes.go](../../internal/engine/recommend_nodes.go)
