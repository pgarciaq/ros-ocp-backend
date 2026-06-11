# ADR-0263: Stop writing workload_metrics in native mode

## Status

Accepted

## Phase

1–2

## Context

Kruize-era ingest wrote raw Prometheus metrics to `workload_metrics` table for later Kruize consumption. Each hourly sample became a row — high storage cost and slow INSERT throughput.

Native engine uses digest buckets (pre-aggregated hourly P50/P95/P99/max/min/sum/count) instead ([ADR-0045](0045-daily-digest-tables-not-raw-metrics.md)).

## Decision

Native ingest path skips `workload_metrics` writes entirely. Digests replace raw metric storage. `workload_metrics` table remains for Kruize plugin backward compatibility but receives no writes when native plugins are active.

## Alternatives Considered

### Keep dual-write (metrics + digests)

Storage cost with no benefit if Kruize is deprecated.

### Delete table immediately

Breaks Kruize rollback path.

## Consequences

- Significant storage reduction (~10× fewer rows).
- Faster ingest (no raw metric INSERT).
- Cannot reconstruct raw time-series from digests (lossy aggregation).
- Kruize rollback would require re-ingest of raw metrics.

## Related Decisions

- [ADR-0001](0001-native-engine-over-kruize.md): Native engine over Kruize.
- [ADR-0045](0045-daily-digest-tables-not-raw-metrics.md): Daily digest tables.
- [ADR-0163](0163-kruize-deprecation-path.md): Kruize deprecation.
- [ADR-0264](0264-kruize-era-legacy-table-background-deletion.md): Legacy table cleanup.

## References

- [internal/processor/digest.go](../../internal/processor/digest.go)
- [migrations/000012_workload_metrics.up.sql](../../migrations/000012_workload_metrics.up.sql)
