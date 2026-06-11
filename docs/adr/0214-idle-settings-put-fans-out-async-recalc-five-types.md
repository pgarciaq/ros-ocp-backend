# ADR-0214: Idle settings PUT fans out async recalc to five recommendation types

## Status

Accepted

## Context

Idle classification affects multiple resource types. Container recommendations include idle notifications; GPU recs check idle GPUs; namespace, node, and PVC all have idle states. A single "idle" recalc type would miss downstream consumers.

## Decision

When idle detection settings are PUT, async threshold recalculation is triggered for container, GPU, namespace, node, AND PVC recommendation types — not a dedicated idle type.

Each type's recalc checks its hash ([ADR-0186](0186-per-cluster-threshold-hash-skip.md)) and skips if unchanged.

## Alternatives Considered

### Single "idle" recalc type

Would miss GPU/PVC idle states tied to separate plugins.

### Event-driven per-plugin recompute

Over-engineering for infrequent settings changes.

## Consequences

- Worker load spikes when idle settings change (5× recalc jobs).
- Hash-skip optimization limits actual work to clusters with changed effective config.
- Engineers may expect one targeted recompute but observe five queued jobs.

## Related Decisions

- [ADR-0186](0186-per-cluster-threshold-hash-skip.md): Per-cluster threshold hash skip.
- [ADR-0173](0173-tenant-configurable-idle-detection.md): Idle settings domain.
- [ADR-0209](0209-dual-idle-threshold-surfaces-container-vs-idle-detection.md): Dual idle surfaces.

## References

- [internal/api/handlers_idle_detection_settings.go](../../internal/api/handlers_idle_detection_settings.go)
- [internal/engine/threshold_recalc.go](../../internal/engine/threshold_recalc.go)
