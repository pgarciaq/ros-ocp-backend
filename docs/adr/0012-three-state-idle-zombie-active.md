# ADR-0012: Use three-state idle/zombie/active classification

## Status

Accepted

## Context

Binary idle-only model couldn't distinguish deletable zombies from low-but-nonzero workloads.

## Decision

Three states based on request-relative utilization: idle (zero usage), zombie (negligible), active.

## Consequences

Enables targeted UI actions (delete zombie vs right-size idle). More notification codes needed.

## References

- [internal/engine/idle_classification.go](internal/engine/idle_classification.go)
- [docs/features/idle-detection.md](docs/features/idle-detection.md)
