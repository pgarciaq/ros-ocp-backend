# ADR-0021: Exclude idle/memory-bound/MIG workloads from time-slicing candidates

## Status

Accepted

## Context

Time-slicing shared GPUs with isolation-sensitive or idle workloads is counterproductive.

## Decision

Filter out idle GPUs, memory-bound workloads, and already-MIG-partitioned GPUs from time-slicing.

## Alternatives Considered

### Include all GPUs in time-slicing candidates
Produces nonsensical recommendations to share idle GPUs or slice MIG partitions further; memory-bound workloads suffer latency regression under time-slicing.

### Separate exclusion rules per metric without unified filter
Inconsistent candidate sets when SM says compute-bound but DRAM says memory-bound; harder to test and explain in UI.

## Related Decisions

- [ADR-0022](0022-timeslicing-replicas-clamp-2-8-majority-rule.md): replica clamp and majority rule apply only to time-slicing candidates filtered here.
- [ADR-0115](0115-gpu-mig-idle-persist-timeslicing-read-time.md): MIG/idle persistence and read-time savings complement these exclusion rules.

## Consequences

Only compute-bound, active, whole-GPU workloads considered. Fewer but higher-quality candidates.

## References

- [internal/engine/gpu_timeslicing.go](internal/engine/gpu_timeslicing.go)
