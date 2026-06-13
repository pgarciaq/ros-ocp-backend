# ADR-0292: Digest-based percentile-band plots with separate sample retention

## Status

Accepted

## Phase

Performance (E-2)

## Context

Raw `container_usage_samples` and `namespace_usage_samples` drive 90%+ of PostgreSQL
disk usage at scale (~96 rows/container/day). [ADR-0055](0055-query-time-boxplots-from-samples.md)
made boxplots the only read path that required retaining raw samples at the same
6-month horizon as daily digests.

Detail API plots used `percentile_cont()` over raw samples at query time, with
6-hour bucketing for short-term terms. That imposed significant read-time CPU and
forced long sample retention even though recommendation math already consumes
pre-aggregated percentiles from `daily_*_digests`.

The native engine performance audit (E-2) identified separating sample retention
from digest retention as the highest-impact disk savings opportunity (estimated
60–80% reduction at 10k+ containers).

## Decision

1. **Replace query-time boxplots** with digest-based percentile-band data:
   - Read `p50`, `p95`, `p99`, and `max` from `daily_container_digests` /
     `daily_namespace_digests` (`schedule_type = 'all_hours'`).
   - All terms use **daily buckets** (short-term loses 6-hour resolution; acceptable
     because the default short-term window is 24h = one daily point).
   - API response shape changes from five-number summary (`min`, `q1`, `median`,
     `q3`, `max`) to `PlotDetails` (`p50`, `p95`, `p99`, `max`, `format`).

2. **Separate sample retention** via `ROS_SAMPLE_RETENTION_DAYS` (default **45**):
   - `container_usage_samples` and `namespace_usage_samples` are swept on this
     shorter horizon.
   - Daily digests continue using `ROS_RETENTION_MONTHS` (default 6).

## Alternatives Considered

### Enrich digests with P25/P75/min and keep boxplot shape
Would preserve the existing five-number summary visualization but adds four columns
per metric per digest row, increases ingest CPU, and still requires raw samples for
accuracy during the current day's digest gap.

### Keep boxplots on samples with shorter sample retention only
Reduces disk but retains expensive `percentile_cont()` aggregation on every detail
API call; digest data already holds the percentiles needed for charts.

### Client-side plotting from raw samples
Rejected in ADR-0055 — payloads too large for namespace history at scale.

## Consequences

- **Positive:** 60–80% disk savings potential by dropping samples after 45 days while
  keeping 6-month digests; detail API plot queries become simple indexed reads.
- **Positive:** Removes the last production read path that required long sample retention.
- **Breaking:** Plot JSON fields change (`PlotDetails` replaces `BoxPlotDetails`);
  UI must render percentile-band charts instead of boxplots.
- **Trade-off:** Short-term plots show one daily point instead of four 6-hour buckets;
  long/medium-term behavior unchanged (already daily).
- **Supersedes:** [ADR-0055](0055-query-time-boxplots-from-samples.md) for plot assembly;
  ADR-0055 remains historical context for why samples were originally retained.

## References

- [internal/model/boxplot.go](../../internal/model/boxplot.go)
- [internal/engine/retention.go](../../internal/engine/retention.go)
- [docs/performance/native-engine-audit-2026-06.md](../performance/native-engine-audit-2026-06.md) (E-2)
