# ADR-0019: Use multi-metric GPU tree (SM, tensor, DRAM) not single SM threshold

## Status

Accepted

## Context

10% SM-only rules mis-classify memory-bound LLM workloads as idle.

## Decision

Decision tree evaluating SM utilization, tensor core activity, and DRAM bandwidth together.

## Consequences

Accurate LLM/training workload classification. More complex classification logic. Requires all three metrics from operator.

## References

- [internal/engine/gpu_recommender.go](internal/engine/gpu_recommender.go)
- [docs/architecture/gpu-classification.md](docs/architecture/gpu-classification.md)
