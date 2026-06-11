# ADR-0172: Dual-path idle classification (authoritative vs legacy fallback)

## Status

Accepted

## Context

Container idle detection evolved from inline fixed-threshold checks ([ADR-0011](0011-fixed-idle-thresholds-10mcpu-10mib.md), [ADR-0013](0013-idle-classify-inline-during-produce.md)) to a first-class classification system with tenant-configurable thresholds ([ADR-0173](0173-tenant-configurable-idle-detection.md)).

Both paths coexist during migration. When `idleClassificationAuthoritative()` returns true—idle detection enabled, namespace not excluded, and sufficient observation days—`ClassifyIdleState()` is authoritative and sets `idle_state` plus notification codes 5 (idle) and 8 (zombie). Otherwise the engine falls back to inline `cpuRec.IsIdle` (decay-window 10 mCPU / 10 MiB) and `DetectAbandoned()`.

## Decision

Maintain two-path coexistence with an authoritative gate in `recommend_all.go`.

- **Authoritative path:** `ClassifyIdleState()` produces zombie (code 8) and idle (code 5). Zombie subsumes abandoned in authoritative mode.
- **Legacy path:** Inline decay-window checks and `DetectAbandoned()` produce abandoned classification when authoritative preconditions are not met.

The `idleClassificationAuthoritative()` function is the single switch between paths.

## Alternatives Considered

### Remove legacy immediately

Breaks clusters with fewer than 14 days of observation history where authoritative classification cannot run.

### Single unified path always

Cannot compute authoritative classification without minimum observation days; would leave early-life containers unclassified.

## Consequences

- Engineers changing idle logic must understand both paths and the authoritative gate.
- Legacy path will be removed once all clusters have sufficient observation history.
- Notification codes and `idle_state` may diverge briefly between paths during migration ([ADR-0174](0174-fleet-summary-idle-via-notification-codes.md)).

## Related Decisions

- [ADR-0011](0011-fixed-idle-thresholds-10mcpu-10mib.md): Original fixed thresholds (defaults only after ADR-0173).
- [ADR-0013](0013-idle-classify-inline-during-produce.md): Inline legacy classification.
- [ADR-0173](0173-tenant-configurable-idle-detection.md): Tenant-configurable idle settings.

## References

- [internal/engine/recommend_all.go](../../internal/engine/recommend_all.go)
- [internal/engine/idle_classification.go](../../internal/engine/idle_classification.go)
- [internal/engine/decay.go](../../internal/engine/decay.go)
