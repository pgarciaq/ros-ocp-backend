# ADR-0055: Use query-time boxplots from container_usage_samples

## Status

Accepted

## Context

Pre-computed plot JSON goes stale and can't adapt to query window.

## Decision

Compute boxplots at query time from raw samples table. Regains accuracy.

## Alternatives Considered

### Pre-computed boxplot JSON stored at ingest
Materializing plot data during ingest avoids read-time CPU, but fixed bucket boundaries cannot adapt when users query custom date ranges or switch between short/medium/long terms—the stored JSON goes stale or requires triple storage per term.

### TimescaleDB continuous aggregates for percentiles
Hypertable rollups would push percentile math into PostgreSQL, but ADR-0002 already rejected TimescaleDB/t-digest for RDS compatibility; pre-aggregated boxplots inherit the same merge-accuracy problems on late-arriving samples.

### Return raw hourly samples to the client
Shipping all ~96 samples/day to the browser offloads compute but balloons API payloads (namespace history with dozens of containers × 90 days); server-side aggregation in `internal/model/boxplot.go` with 6h/daily bucketing (ADR-0056) keeps responses under gzip threshold.

## Consequences

Fresh data on every query. Compute cost on read. Requires samples retention.

## References

- [internal/model/boxplot.go](internal/model/boxplot.go)
