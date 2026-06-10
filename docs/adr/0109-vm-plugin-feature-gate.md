# ADR-0109: Gate VM plugin with ROS_ENABLE_VM_RECS

## Status

Accepted

## Context

VM support depends on operator CSV maturity not yet GA.

## Decision

VM plugin behind explicit feature gate until operator stabilizes.

## Consequences

No VM processing unless opted in. Safe default. Can enable per-cluster.

## References

- [internal/plugins/vm/plugin.go](internal/plugins/vm/plugin.go)
