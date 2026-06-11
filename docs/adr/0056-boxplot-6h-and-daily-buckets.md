# ADR-0056: Use 6-hour buckets (short term) and daily buckets (medium/long)

## Status

Accepted

## Context

Uniform hourly boxplots for long windows generate too many data points and noise.

## Decision

Short term: 6-hour granularity (4 points/day). Medium/long term: daily granularity. For a 90-day window, 6-hour buckets yield ~360 points vs ~2160 hourly—enough pattern visibility without chart noise or query cost of uniform hourly buckets.

## Alternatives Considered

### Uniform hourly buckets
2160 points over 90 days overwhelms client rendering and boxplot SQL; marginal detail beyond 6-hour resolution rarely changes right-sizing decisions.

### Variable-width buckets by term
Flexible but complex client rendering and harder to document; API consumers must implement width-aware chart logic.

## Consequences

Balanced detail vs performance. Different visual resolution per term.

## References

- [internal/model/boxplot.go](internal/model/boxplot.go)
