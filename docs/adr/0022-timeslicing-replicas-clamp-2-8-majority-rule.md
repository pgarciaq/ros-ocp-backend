# ADR-0022: Clamp time-slicing replicas to [2, 8] with majority ≥50% candidate rule

## Status

Accepted

## Context

Unbounded replica counts and minority-driven recommendations are unreliable.

## Decision

Cap replicas at 8, floor at 2; require ≥50% of node's GPU workloads to be candidates.

## Consequences

Conservative but reliable time-slicing recommendations. Won't over-subscribe sparse nodes.

## References

- [internal/engine/gpu_timeslicing.go](internal/engine/gpu_timeslicing.go)
