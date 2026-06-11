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

## Status Update (2026-06)

The fixed 10mCPU/10MiB thresholds described above are now **defaults only**. [ADR-0173](0173-tenant-configurable-idle-detection.md) documents the tenant-configurable idle detection system that supersedes these fixed values. The decay-window inline detection remains as a legacy fallback ([ADR-0172](0172-dual-path-idle-classification.md)) for clusters with insufficient observation history.
