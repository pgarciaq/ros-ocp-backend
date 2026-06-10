# ADR-0055: Use query-time boxplots from container_usage_samples

## Status

Accepted

## Context

Pre-computed plot JSON goes stale and can't adapt to query window.

## Decision

Compute boxplots at query time from raw samples table. Regains accuracy.

## Consequences

Fresh data on every query. Compute cost on read. Requires samples retention.

## References

- [internal/model/boxplot.go](internal/model/boxplot.go)
