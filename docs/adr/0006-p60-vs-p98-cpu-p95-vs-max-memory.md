# ADR-0006: Use P60 (cost) vs P98 (perf) for CPU, P95 vs max for memory

## Status

Accepted

## Context

Single-profile sizing can't satisfy both cost optimization and reliability goals.

## Decision

Cost engine uses P60 CPU / P95 memory; performance engine uses P98 CPU / max memory.

## Consequences

Exposes clear trade-off to user. Requires engine-aware UI. Different sizing profiles per resource type.

## References

- [internal/engine/recommend_cpu.go](internal/engine/recommend_cpu.go)
- [internal/engine/recommend_memory.go](internal/engine/recommend_memory.go)
