# ADR-0254: PVC growth projection via decay-weighted slope

## Status

Accepted

## Context

Near-full PVC notifications must help operators plan capacity before outage. [ADR-0044](0044-linear-regression-trend-2-day-minimum.md) uses linear regression for namespace/container trends. PVC usage history spans longer terms with zero decay on long windows ([ADR-0027](0027-pvc-longer-terms-zero-decay.md)).

[ADR-0204](0204-continuous-hour-decay-vs-calendar-day-windows.md) distinguishes continuous-hour decay from calendar-day aggregation — PVC growth projection applies decay-weighted regression consistent with engine decay philosophy.

## Decision

`computePVCGrowthSlope` calculates a **decay-weighted linear regression slope** over historical PVC usage samples. Project **days-to-full** as:

```
days_to_full = (capacity - current_usage) / max(slope, epsilon)
```

Gated by `min(trend_days, MinTrendDays)` — insufficient history suppresses projection.

**Near-full** threshold notifications include projected **days-to-full** when slope is positive and statistically meaningful, giving operators lead time beyond static utilization percent.

## Alternatives Considered

### Simple linear regression unweighted

Recent spikes overweight stale history equally; poor near-term forecast.

### Calendar-day buckets only

Misaligns with digest hourly granularity for short near-full windows.

### No projection in notification

Operators only see "% full" without timeline.

## Consequences

- Flat or negative slope omits days-to-full field — not an error.
- Min trend days prevents noisy projection on newly observed PVCs.
- Decay parameters align with container engine half-life config where shared.

## Related Decisions

- [ADR-0025](0025-pvc-thresholds-20-oversized-85-near-full.md): Near-full threshold.
- [ADR-0204](0204-continuous-hour-decay-vs-calendar-day-windows.md): Decay vs calendar windows.
- [ADR-0253](0253-pvc-four-way-classification-healthy-orphaned.md): Near-full classification.

## References

- [internal/plugins/pvc/trend.go](../../internal/plugins/pvc/trend.go)
- [internal/engine/decay.go](../../internal/engine/decay.go)
