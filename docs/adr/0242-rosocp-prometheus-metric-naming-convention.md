# ADR-0242: rosocp_ Prometheus metric naming convention

## Status

Accepted

## Context

Multiple processes (API, processor, housekeeper) expose Prometheus metrics on `/metrics` ([ADR-0130](0130-shallow-readyz-default.md)). Without a consistent prefix, metric collisions with other platform exporters and ambiguous subsystem ownership complicate Grafana dashboards and alert routing.

Instrumentation coverage varies by subsystem — ingest and cache paths are heavily instrumented; some API handlers (e.g., settings PUT) lack latency histograms.

## Decision

All application metrics use the **`rosocp_` prefix**. Subsystem appears in the metric name:

```
rosocp_{subsystem}_{metric}_{unit}
```

Examples:

- `rosocp_ingest_rows_processed_total`
- `rosocp_fleet_summary_cache_hits_total`
- `rosocp_savings_summary_cache_misses_total`
- `rosocp_reship_coalesced_total`

Counter suffix `_total`, histogram suffix `_seconds` or `_bytes` follow Prometheus naming conventions. Not every subsystem is equally instrumented — gaps are acceptable until hot paths justify cardinality cost.

## Alternatives Considered

### Unprefixed metrics

Collides with node_exporter and sidecar metrics in shared scrape configs.

### Prefix per binary (rosocp_api_, rosocp_processor_)

Fragments dashboards when aggregating fleet-wide ROS behavior.

### OpenTelemetry metrics instead

Not adopted; Prometheus remains operational standard ([ADR-0130](0130-shallow-readyz-default.md)).

## Consequences

- New metrics must pass naming review for prefix and subsystem segment.
- External SLO dashboards filter `{__name__=~"rosocp_.*"}`.
- Missing histograms on low-traffic handlers documented as known gaps, not bugs.

## Related Decisions

- [ADR-0130](0130-shallow-readyz-default.md): Health and metrics endpoints.
- [ADR-0243](0243-high-cardinality-analytics-incomplete-labels.md): Exception for analytics_incomplete labels.
- [ADR-0112](0112-bounded-lru-ttl-cost-cache.md): Cache hit/miss metrics pattern.

## References

- [internal/metrics/metrics.go](../../internal/metrics/metrics.go)
- [docs/operations/monitoring.md](../operations/monitoring.md)
