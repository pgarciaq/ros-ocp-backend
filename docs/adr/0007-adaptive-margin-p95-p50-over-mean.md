# ADR-0007: Use adaptive margin from (P95-P50)/mean clamped 1.15-1.50

## Status

Accepted

## Context

Fixed margins over-provision stable workloads and under-provision bursty ones.

## Decision

Compute variability ratio `(P95 − P50) / mean`, clamp to [1.15, 1.50], apply as multiplier to base recommendation. The 1.15 floor prevents zero-margin recommendations on perfectly flat workloads; the 1.50 cap prevents runaway headroom when mean is near zero.

## Alternatives Considered

### Fixed 20% margin
Stable workloads receive unnecessary headroom while bursty workloads still under-provision during spikes. Rejected after comparison showed fixed margin over-provisioned low-variance containers by ~15–20% on average.

### No margin (raw percentile only)
Bursty workloads OOM or throttle because a single percentile snapshot misses intra-hour spikes. Rejected because production clusters showed restart correlation with recommendations that lacked variability adjustment.

## Consequences

Bursty workloads get more headroom automatically. Stable workloads tighter. Adds compute cost per recommendation.

## References

- [internal/engine/margin.go](internal/engine/margin.go)
