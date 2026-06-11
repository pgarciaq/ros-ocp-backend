# ADR-0208: Settings scope is org-wide only (not hierarchical) except business hours

## Status

Accepted

## Context

The settings system stores thresholds in `recommendation_thresholds` keyed by `(org_id, recommendation_type)`. Despite [ADR-0084](0084-three-tier-settings-precedence.md) suggesting cluster/namespace overrides, the schema has no `cluster_uuid` or `namespace` columns. Only business hours uses hierarchical scoping (org → cluster → namespace).

## Decision

All threshold settings (container, GPU, namespace, node, PVC, idle detection, quota, VM, snapshot) are org-wide. Resolution order: env lock → DB row → compiled defaults. No per-cluster or per-namespace overrides for thresholds.

Business hours is the sole exception — it uses scoped rows with `cluster_uuid`/`namespace` in `business_hour_schedules`.

## Alternatives Considered

### Add cluster/namespace columns to thresholds

Requires schema change and complex resolution precedence across scopes.

### Move business hours to org-only

Loses granularity operators need for cluster- and namespace-specific schedules.

## Consequences

- [ADR-0084](0084-three-tier-settings-precedence.md) is partially misleading — it describes intended future behavior, not current schema.
- Operators cannot set different thresholds per cluster within one org.
- Business hours inheritance (org → cluster → namespace) is a separate code path (`bhschedule.Cache.Resolve()`).

## Related Decisions

- [ADR-0084](0084-three-tier-settings-precedence.md): Three-tier precedence — needs correction for scope.
- [ADR-0173](0173-tenant-configurable-idle-detection.md): Idle detection settings domain.
- [ADR-0186](0186-per-cluster-threshold-hash-skip.md): Threshold recalc hash skip.

## References

- [internal/api/handlers_threshold_settings.go](../../internal/api/handlers_threshold_settings.go)
- [internal/bhschedule/cache.go](../../internal/bhschedule/cache.go)
