# ADR-0023: Use GPU confidence from data volume + burst penalty

## Status

Accepted

## Context

Need to signal unreliable classifications on sparse or bursty GPU data.

## Decision

Confidence score combines data volume (coverage within the lookback window) with burst penalty: when peak SM utilization exceeds mean + 2σ, confidence drops because sparse snapshots miss spike behavior.

## Alternatives Considered

### Fixed confidence after N days
Ignores workload variability—a bursty training job with 14 days of data still misleads if spikes occur between samples. Rejected because operators treated high day-count as reliability signal.

### No confidence signal
Users cannot distinguish "well-measured idle GPU" from "maybe idle because we only have 6 hours of data." Rejected because FinOps workflows need explicit reliability cues before acting on GPU recommendations.

## Consequences

Users see confidence on GPU recommendations. Sparse data gets low confidence. Bursty data penalized.

## References

- [docs/architecture/gpu-classification.md](docs/architecture/gpu-classification.md)
- [internal/engine/gpu_recommender.go](internal/engine/gpu_recommender.go)
