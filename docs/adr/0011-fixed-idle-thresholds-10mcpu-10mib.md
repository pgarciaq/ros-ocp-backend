# ADR-0011: Use fixed 10 mCPU / 10 MiB idle thresholds (not env-configurable)

## Status

Accepted

## Context

Needed simple idle detection for MVP without per-tenant tuning complexity.

## Decision

Hardcode idle thresholds at 10 mCPU and 10 MiB. Extended later by three-state classifier.

## Consequences

Simple implementation. Not tunable per tenant. Sufficient for initial idle detection.

## References

- [internal/engine/detect_idle.go](internal/engine/detect_idle.go)
