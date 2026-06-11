# ADR-0204: Continuous-hour decay vs calendar-day windows

## Status

Accepted

> **Note:** This ADR documents a straightforward or inherited decision kept for completeness and historical traceability. It does not represent a non-obvious architectural fork.

## Context

Recommendation algorithms weight recent data more heavily using exponential decay ([ADR-0005](0005-decay-weighted-average-half-life.md)). The decay function needs a time reference — either calendar-day boundaries or continuous hours from observation to present.

## Decision

**Decay weights** use continuous hours from `BucketDate` to `now`, explicitly **not** calendar-day boundaries (~1h skew acceptable vs fixed midnight boundaries).

**Window filtering** uses inclusive day truncation with binary search (`filterByWindow`) on sorted bucket dates — O(log n) for window application.

The same data point gets slightly different weights depending on time of day.

## Consequences

- Small (~1h) time-of-day variation in decay weights — acceptable for recommendation stability.
- Simpler implementation (no timezone/DST handling).
- Binary search on sorted bucket dates is O(log n) for window application.

## Alternatives Considered

### Calendar-day boundaries

Requires timezone awareness and DST handling. Rejected.

### Fixed 24h blocks

Artificial boundaries create discontinuities at block edges. Rejected.

## Related Decisions

- [ADR-0005](0005-decay-weighted-average-half-life.md): Decay-weighted P95 baseline.
- [ADR-0171](0171-streaming-recommendation-batches.md): Streaming batches for memory bounding.

## References

- [internal/engine/decay.go](../../internal/engine/decay.go)
- [internal/engine/recommend_all.go](../../internal/engine/recommend_all.go)
