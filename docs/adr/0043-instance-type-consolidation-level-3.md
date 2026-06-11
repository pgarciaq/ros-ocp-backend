# ADR-0043: Use instance-type consolidation Level 3 when instance_type present

## Status

Accepted

## Context

Homogeneous instance-type fleets enable fleet-level consolidation math beyond per-node.

## Decision

When `instance_type` annotation is available, compute Level 3 fleet-wide consolidation.

## Consequences

More aggressive savings for uniform fleets. Only works with instance-type metadata from operator.

## References

- [internal/engine/recommend_nodes.go](internal/engine/recommend_nodes.go)

## Status Update (2026-06)

[ADR-0194](0194-node-consolidation-precedence-pod-scheduling-gate.md) extends the consolidation model with: (1) fleet grouping precedence (MachineSet > instance_type > capacity key), (2) pod-scheduling gate (`podSchedulingBlocksConsolidation`) that prevents recommendations exceeding pod headroom, and (3) notification code 76 for MachineSet-group reduction opportunities.
