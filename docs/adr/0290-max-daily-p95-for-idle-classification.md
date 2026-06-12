# ADR-0290: Max-of-daily-P95 for idle classification

## Status

Accepted

## Phase

Engine / Algorithm (performance)

## Context

Container and GPU idle classification ([ADR-0012](0012-three-state-idle-zombie-active.md),
[ADR-0172](0172-dual-path-idle-classification.md)) aggregates daily digest rows over
an observation window (typically 14 days). The authoritative path computed a window
P95 by sorting all daily P95 values — O(N log N) with slice allocation per container.

The native engine performance audit (Q4) identified this as wasteful: idle detection
only needs to know whether utilization stayed near zero across the window, not the
exact 95th percentile of daily aggregates. Node idle classification already used
max-of-daily-P95 in `recommend_nodes.go`.

## Decision

Replace window P95 sorting with **max of daily P95 values** for container and GPU
idle classification:

1. `ClassifyIdleState` uses `maxDailyCPUUsageP95` / `maxDailyMemUsageP95` over
   `DigestRow.CPUUsageP95MC` and `MemUsageP95KiB`.
2. `ClassifyGPUIdleFromDigests` uses `maxDailyGPUField` over daily `SMActiveAvg`
   and `DRAMActiveAvg`.
3. Per-day idle-since predicates still use each day's own P95 fields (unchanged).

**Conservative bound:** `max(daily P95) ≥ P95(daily P95)`. If max is below the idle
threshold, the true window P95 is also below it — no false positives (active
containers misclassified as idle). The trade-off is false negatives: a single-day
spike can prevent idle classification even when the window P95 would have been low.

## Alternatives Considered

### Keep exact window P95 sort

Correct but allocates and sorts ~15 values per container twice (CPU + memory) on
every recommendation cycle. Unnecessary precision for a threshold comparison.

### Decay-weighted max

More complex; idle detection already has a separate burst-ratio guard on peak vs
aggregated utilization.

## Consequences

- Idle/zombie classification may classify fewer workloads as idle when historical
  spikes exist on a minority of days.
- Eliminates two O(N log N) sorts per container in `ClassifyIdleState` and two
  per GPU in `ClassifyGPUIdleFromDigests`; O(N) scan with no allocations.
- Engineers tuning idle thresholds should understand max-of-daily-P95 semantics.

## Related Decisions

- [ADR-0006](0006-p60-vs-p98-cpu-p95-vs-max-memory.md): Recommendation sizing uses
  different percentile choices; idle is a separate path.
- [ADR-0172](0172-dual-path-idle-classification.md): Authoritative idle gate unchanged.

## References

- [internal/engine/idle_classification.go](../../internal/engine/idle_classification.go)
- [internal/engine/gpu_idle_classification.go](../../internal/engine/gpu_idle_classification.go)
- [docs/performance/native-engine-audit-2026-06.md](../performance/native-engine-audit-2026-06.md) (Q4)
