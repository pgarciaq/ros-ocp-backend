# ADR-0173: Tenant-configurable idle detection supersedes fixed thresholds

## Status

Accepted

## Context

[ADR-0011](0011-fixed-idle-thresholds-10mcpu-10mib.md) described fixed 10 mCPU / 10 MiB idle thresholds. Diverse workloads and platform namespaces require per-org, per-cluster, and per-namespace tuning. Idle detection is now a first-class settings domain (`recommendation_type = idle_detection`).

## Decision

Idle settings follow the same three-tier precedence as sizing ([ADR-0084](0084-three-tier-settings-precedence.md)): env lock → DB (org / cluster / namespace) → `DefaultIdleConfig()`.

Configurable parameters include:

- Utilization-% thresholds (CPU 2%, memory 5%)
- Burst-ratio and minimum observation days (14)
- Namespace exclusions (`kube-system`, `openshift-*`)
- Workload-type exclusions (`DaemonSet`)
- GPU basis-point thresholds

Settings changes trigger async threshold recalculation ([ADR-0186](0186-per-cluster-threshold-hash-skip.md)).

## Alternatives Considered

### Keep fixed thresholds

Insufficient for diverse workloads and platform namespaces.

### ML-based adaptive thresholds

Too complex for v1; reserved for future consideration.

## Consequences

- ADR-0011 fixed thresholds are defaults only, not hardcoded behavior.
- Changing idle settings triggers async threshold recalculation across affected clusters.
- Namespace exclusions prevent platform namespaces from being flagged idle.
- Authoritative classification gate depends on resolved settings ([ADR-0172](0172-dual-path-idle-classification.md)).

## Related Decisions

- [ADR-0011](0011-fixed-idle-thresholds-10mcpu-10mib.md): Superseded as fixed-only policy; thresholds remain as defaults.
- [ADR-0172](0172-dual-path-idle-classification.md): Authoritative gate uses resolved idle config.
- [ADR-0186](0186-per-cluster-threshold-hash-skip.md): Hash-based skip for idle threshold recalc.

## References

- [internal/engine/idle_settings.go](../../internal/engine/idle_settings.go)
- [internal/engine/idle_classification.go](../../internal/engine/idle_classification.go)
- [internal/api/handlers_idle_detection_settings.go](../../internal/api/handlers_idle_detection_settings.go)
