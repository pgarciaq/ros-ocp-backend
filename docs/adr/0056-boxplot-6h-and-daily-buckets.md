# ADR-0056: Use 6-hour buckets (short term) and daily buckets (medium/long)

## Status

Accepted

## Context

Uniform hourly boxplots for long windows generate too many data points and noise.

## Decision

Short term: 6-hour granularity. Medium/long term: daily granularity.

## Consequences

Balanced detail vs performance. Different visual resolution per term.

## References

- [internal/model/boxplot.go](internal/model/boxplot.go)
