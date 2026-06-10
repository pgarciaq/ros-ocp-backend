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
