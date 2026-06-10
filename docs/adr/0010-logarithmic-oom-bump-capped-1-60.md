# ADR-0010: Use logarithmic OOM bump capped at 1.60×

## Status

Accepted

## Context

Linear bumps over-react to rare OOM spikes; uncapped bumps waste resources.

## Decision

On OOM detection, bump memory recommendation logarithmically, capped at 1.60× current.

## Consequences

Dampens OOM over-reaction. Still responsive to genuine memory pressure. Cap prevents runaway sizing.

## References

- [internal/engine/recommend_memory.go](internal/engine/recommend_memory.go)
