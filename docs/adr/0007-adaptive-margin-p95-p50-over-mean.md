# ADR-0007: Use adaptive margin from (P95-P50)/mean clamped 1.15-1.50

## Status

Accepted

## Context

Fixed margins over-provision stable workloads and under-provision bursty ones.

## Decision

Compute variability ratio, clamp to [1.15, 1.50] range, apply as multiplier to base recommendation.

## Consequences

Bursty workloads get more headroom automatically. Stable workloads tighter. Adds compute cost per recommendation.

## References

- [internal/engine/margin.go](internal/engine/margin.go)
