# ADR-0243: High-cardinality labels on analytics_incomplete metric

## Status

Accepted

## Context

[ADR-0062](0062-analytics-incomplete-flag-on-failure.md) marks clusters when history or quality writes fail. Operators and support need to identify **which org and cluster** failed and **why**, without querying logs for every incident.

Prometheus best practice avoids `org_id` and `cluster_uuid` labels on high-frequency metrics — cardinality explodes with fleet scale ([ADR-0242](0242-rosocp-prometheus-metric-naming-convention.md)).

## Decision

`rosocp_analytics_incomplete_total` includes labels:

- `org_id`
- `cluster_uuid`
- `error_type`

This is an **exception** to the general rule of no org/cluster labels on ROS metrics.

Rationale:

1. The counter increments only on analytics failures (rare compared to ingest volume).
2. Cardinality is bounded by active org × cluster pairs that experienced failure, not every request.
3. Support triage requires dimensional breakdown without log correlation for every ticket.

All other metrics avoid org/cluster labels; use aggregated counters or logs with request_id ([ADR-0244](0244-request-correlation-echo-request-id.md)).

## Alternatives Considered

### Log-only failure detail

Slow support workflow; no alertable time series per error class.

### org_id label only, no cluster

Insufficient when one cluster in an org repeatedly fails quality writes.

### Low-cardinality error_type only

Cannot pinpoint cluster for remediation runbooks.

## Consequences

- Alert rules should use `increase()` over windows, not raw counter rate, to limit series churn after recovery.
- New high-cardinality labels require ADR amendment — do not copy this pattern casually.
- Dashboards may top-N by cluster for open incidents.

## Related Decisions

- [ADR-0242](0242-rosocp-prometheus-metric-naming-convention.md): Metric naming convention.
- [ADR-0062](0062-analytics-incomplete-flag-on-failure.md): analytics_incomplete flag.
- [ADR-0180](0180-analytics-write-ordering-strict-mode.md): Analytics write ordering.

## References

- [internal/metrics/analytics.go](../../internal/metrics/analytics.go)
- [internal/processor/analytics.go](../../internal/processor/analytics.go)
