# ADR-0023: Use GPU confidence from data volume + burst penalty

## Status

Accepted

## Context

Need to signal unreliable classifications on sparse or bursty GPU data.

## Decision

Confidence score combines data volume (days/window) with burst penalty (max SM > 5× avg penalizes).

## Consequences

Users see confidence on GPU recommendations. Sparse data gets low confidence. Bursty data penalized.

## References

- [docs/architecture/gpu-classification.md](docs/architecture/gpu-classification.md)
- [internal/engine/gpu_recommender.go](internal/engine/gpu_recommender.go)
