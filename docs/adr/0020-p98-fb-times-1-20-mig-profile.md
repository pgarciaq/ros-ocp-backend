# ADR-0020: Use P98 FB × 1.20 headroom for MIG profile selection

## Status

Accepted

## Context

Need to size GPU memory partitions to peak usage, not average.

## Decision

Take P98 of framebuffer usage, multiply by 1.20 headroom, match to nearest MIG profile.

## Consequences

Avoids OOM in MIG partitions. May slightly over-provision memory-light workloads.

## References

- [internal/engine/gpu_recommender.go](internal/engine/gpu_recommender.go)
