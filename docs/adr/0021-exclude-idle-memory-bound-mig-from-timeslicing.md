# ADR-0021: Exclude idle/memory-bound/MIG workloads from time-slicing candidates

## Status

Accepted

## Context

Time-slicing shared GPUs with isolation-sensitive or idle workloads is counterproductive.

## Decision

Filter out idle GPUs, memory-bound workloads, and already-MIG-partitioned GPUs from time-slicing.

## Consequences

Only compute-bound, active, whole-GPU workloads considered. Fewer but higher-quality candidates.

## References

- [internal/engine/gpu_timeslicing.go](internal/engine/gpu_timeslicing.go)
