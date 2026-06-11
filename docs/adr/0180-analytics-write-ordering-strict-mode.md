# ADR-0180: Analytics write ordering (recommendations-first vs strict mode)

## Status

Accepted

## Context

The analytics pipeline writes three artifacts per recommendation run: live recommendations, history, and quality scores ([ADR-0062](0062-analytics-incomplete-flag-on-failure.md)). Partial failures can leave artifacts inconsistent. Operators need a choice between fast recommendation visibility and strict consistency.

## Decision

Two write orderings controlled by `ROS_INGEST_STRICT_ANALYTICS` (now default `true`):

- **Non-strict (legacy):** Write recommendations first, then history/quality. History/quality failures set `analytics_incomplete` but do not roll back recommendations.
- **Strict (default):** Write analytics (history/quality) first; abort the batch on analytics failure so recommendations are not visible without supporting analytics rows.

The `analytics_incomplete` flag surfaces in API detail responses when non-strict partial failure occurs.

## Alternatives Considered

### Transactional all-or-nothing

Too costly for write volume and cross-table scope.

### Eventual consistency with background retry

Adds complexity without guaranteeing visibility timing.

## Consequences

- Strict mode delays recommendation visibility when analytics writes fail (safer).
- Non-strict mode may expose recommendations without quality rows after partial failures.
- Ingest operators must understand the flag when debugging missing quality data ([ADR-0096](0096-strict-analytics-mode-optional.md)).

## Related Decisions

- [ADR-0096](0096-strict-analytics-mode-optional.md): Strict analytics flag introduction.
- [ADR-0062](0062-analytics-incomplete-flag-on-failure.md): `analytics_incomplete` marker.

## References

- [internal/engine/analytics_pipeline.go](../../internal/engine/analytics_pipeline.go)
