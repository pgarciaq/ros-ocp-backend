# ADR-0179: Recommendation quality stability formula

## Status

Accepted

## Context

Quality scoring tracks how stable recommendations are over time. Stability helps users distinguish recommendations that are still converging from those that consistently target the same resource allocation ([ADR-0037](0037-adoption-detection-5-percent-tolerance.md) covers adoption, not day-over-day target variance).

## Decision

Compute stability as:

```
stability_pct = max(0, 1 − |cpuVariation|/100×0.5 − |memVariation|/100×0.5)
```

- Written daily with `measured_at` truncated to UTC midnight
- Prior recommendation read from short-term row only before overwrite
- Prometheus gauges expose 0–100; DB stores 0–1 float
- Stability is per-engine-per-term, not aggregated across engines

## Alternatives Considered

### Rolling average of variation

Smooths spikes but delays detection of instability.

### Boolean stable/unstable flag

Loses granularity needed for quality dashboards and filtering.

## Consequences

- Large swings between recommendation runs lower stability.
- New recommendations always start with stability 0 (no prior comparison).
- Adoption events interact with quality rows independently per engine ([ADR-0181](0181-adoption-detection-all-term-engine-rows.md)).

## Related Decisions

- [ADR-0037](0037-adoption-detection-5-percent-tolerance.md): Adoption tolerance for applied recommendations.

## References

- [internal/engine/quality.go](../../internal/engine/quality.go)
- [internal/api/handlers_quality.go](../../internal/api/handlers_quality.go)
