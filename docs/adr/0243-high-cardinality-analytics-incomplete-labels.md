# ADR-0243: High-cardinality labels on analytics_incomplete metric

## Status

Superseded by [CHANGELOG](../CHANGELOG.md) (Unreleased) — fleet-wide Prometheus cardinality audit (C-3)

## Context

[ADR-0062](0062-analytics-incomplete-flag-on-failure.md) marks clusters when history or quality writes fail. Operators and support need to identify **which org and cluster** failed and **why**, without querying logs for every incident.

Prometheus best practice avoids `org_id` and `cluster_uuid` labels on high-frequency metrics — cardinality explodes with fleet scale ([ADR-0242](0242-rosocp-prometheus-metric-naming-convention.md)).

## Decision (original)

`rosocp_analytics_incomplete_total` included labels:

- `org_id`
- `cluster_uuid`
- `error_type`

This was an **exception** to the general rule of no org/cluster labels on ROS metrics.

## Superseded decision (2026)

The exception was removed. `rosocp_analytics_incomplete_total` now carries only `error_type`. Per-org/cluster context is logged structurally at increment sites (`internal/engine/analytics_pipeline.go`). The same cardinality reduction applies to all other fleet metrics that previously carried tenant labels — see CHANGELOG Unreleased.

Rationale for reversal:

1. At SaaS scale (1000+ orgs × 5+ clusters), even low-frequency failure metrics create thousands of persistent time series.
2. Structured logging with `org_id` and `cluster_uuid` fields already exists at call sites.
3. Alerting on `increase(rosocp_analytics_incomplete_total{error_type="quality"}[1h])` remains valid without per-tenant series.

## Related Decisions

- [ADR-0242](0242-rosocp-prometheus-metric-naming-convention.md): Metric naming convention.
- [ADR-0062](0062-analytics-incomplete-flag-on-failure.md): analytics_incomplete flag.
- [ADR-0180](0180-analytics-write-ordering-strict-mode.md): Analytics write ordering.

## References

- [internal/metrics/metrics.go](../../internal/metrics/metrics.go)
- [internal/engine/analytics_pipeline.go](../../internal/engine/analytics_pipeline.go)
