# ADR-0019: Use multi-metric GPU tree (SM, tensor, DRAM) not single SM threshold

## Status

Accepted

## Context

10% SM-only rules mis-classify memory-bound LLM workloads as idle.

## Decision

Decision tree evaluating SM utilization, tensor core activity, and DRAM bandwidth together.

## Alternatives Considered

### SM-only classification
Memory-bound LLM inference shows low SM utilization but high DRAM traffic; SM-only rules mis-label these as idle and recommend time-slicing or scale-down incorrectly.

### Flat metric list (no hierarchy)
Evaluating metrics in arbitrary order produces conflicting labels when SM and DRAM disagree; no deterministic tie-break for operators auditing classifications.

### Single composite score
Collapses interpretability—support cannot explain why a workload is "memory-bound" vs "compute-bound" without decomposing the score.

## Consequences

Accurate LLM/training workload classification. More complex classification logic. Requires all three metrics from operator.

## References

- [internal/engine/gpu_recommender.go](internal/engine/gpu_recommender.go)
- [docs/architecture/gpu-classification.md](docs/architecture/gpu-classification.md)
