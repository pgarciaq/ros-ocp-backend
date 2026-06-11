# ADR-0220: BH PUT triggers reship; threshold PUT triggers recalc (not reship)

## Status

Accepted

## Context

Different settings changes require different async operations. Business hours changes need historical data reshipped from Koku. Threshold changes need recommendations recomputed from existing digest data.

## Decision

- Business hours PUT/DELETE → triggers reship pipeline ([ADR-0218](0218-org-level-reship-single-flight-trailing-batch-coalescing.md)).
- Threshold PUT → triggers recalc pipeline ([ADR-0186](0186-per-cluster-threshold-hash-skip.md)).

These are distinct pipelines with different guards, workers, and side effects. They do NOT trigger each other.

## Alternatives Considered

### Single "reprocess everything" pipeline

Wasteful — threshold changes do not require S3 re-aggregation.

### Detect change type and route automatically

Complex detection logic with overlapping side effects.

## Consequences

- Operators must understand which setting change triggers which pipeline.
- BH changes are slower (depends on Koku/Masu processing).
- Threshold changes are faster (local recomputation only).

## Related Decisions

- [ADR-0186](0186-per-cluster-threshold-hash-skip.md): Threshold recalc.
- [ADR-0187](0187-savings-only-recalc-vs-full-threshold-recalc.md): Savings-only vs full recalc.
- [ADR-0218](0218-org-level-reship-single-flight-trailing-batch-coalescing.md): Reship guard.

## References

- [internal/api/handlers_business_hours.go](../../internal/api/handlers_business_hours.go)
- [internal/api/handlers_threshold_settings.go](../../internal/api/handlers_threshold_settings.go)
