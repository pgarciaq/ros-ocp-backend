# ADR-0006: Use P60 (cost) vs P98 (perf) for CPU, P95 vs max for memory

## Status

Accepted

## Context

Single-profile sizing can't satisfy both cost optimization and reliability goals.

## Decision

Cost engine uses P60 CPU / P95 memory; performance engine uses P98 CPU / max memory.

## Alternatives Considered

### Mean-based sizing for both resources
Using arithmetic mean CPU and memory would smooth charts but masks brief spikes that drive throttling and OOM kills; FinOps users reported mean-based recommendations consistently under-provisioned bursty Java and ML workloads.

### Single P95 percentile for CPU and memory
Applying P95 uniformly simplifies configuration, but P95 CPU over-provisions steady-state containers (wasting cluster capacity on cost engine) while still under-sizing memory relative to true peaks that cause cgroup OOM.

### Symmetric percentile pairs (e.g., P60/P98 for both CPU and memory)
Symmetric profiles ignore that CPU throttling is gradual whereas memory failure is cliff-edge; the performance engine needs `max` memory from digest samples (`recommend_memory.go`) to prevent OOM, while CPU headroom is adequately captured at P98 without storing unbounded spike samples.

## Consequences

Exposes clear trade-off to user. Requires engine-aware UI. Different sizing profiles per resource type.

## References

- [internal/engine/recommend_cpu.go](internal/engine/recommend_cpu.go)
- [internal/engine/recommend_memory.go](internal/engine/recommend_memory.go)
