# ADR-0014: Aggregate namespace idle after container+GPU (plugin priority 90)

## Status

Accepted

## Context

Namespace idle state depends on child workload classifications.

## Decision

Namespace plugin runs at priority 90 (after container at 10, GPU at 20) to read completed child states.

## Consequences

Correct aggregation ordering. Namespace can't run in parallel with container.

## References

- [internal/plugins/namespace/plugin.go](internal/plugins/namespace/plugin.go)
