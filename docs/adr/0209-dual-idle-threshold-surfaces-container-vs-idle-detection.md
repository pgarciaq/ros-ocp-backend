# ADR-0209: Dual idle-threshold surfaces (container sizing vs idle_detection settings domain)

## Status

Accepted

## Context

Container sizing thresholds expose `idle_cpu_threshold_mc` and `idle_mem_threshold_kib` (legacy inline idle). The newer `idle_detection` settings domain ([ADR-0173](0173-tenant-configurable-idle-detection.md)) provides utilization-%, burst-ratio, min observation days, and related parameters. Both coexist in production.

## Decision

Both settings surfaces exist. Container sizing idle thresholds feed the legacy inline path ([ADR-0172](0172-dual-path-idle-classification.md) fallback). Idle detection settings feed the authoritative classifier.

Changing idle_detection settings triggers async recalc for container, GPU, namespace, node, AND PVC recommendation types — not a dedicated idle plugin.

## Alternatives Considered

### Remove legacy immediately

Breaks clusters with insufficient data for authoritative classification.

### Unified single settings domain

Requires careful migration of all consumers and UI surfaces.

## Consequences

- Two UI/API paths affect idle behavior.
- The authoritative gate (`idleClassificationAuthoritative`) determines which settings are used at runtime.
- Until legacy removal, both surfaces must be maintained and documented.

## Related Decisions

- [ADR-0172](0172-dual-path-idle-classification.md): Dual-path idle classification.
- [ADR-0173](0173-tenant-configurable-idle-detection.md): Tenant-configurable idle detection.
- [ADR-0214](0214-idle-settings-put-fans-out-async-recalc-five-types.md): Idle PUT fan-out recalc.

## References

- [internal/engine/idle_classification.go](../../internal/engine/idle_classification.go)
- [internal/api/handlers_idle_detection_settings.go](../../internal/api/handlers_idle_detection_settings.go)
