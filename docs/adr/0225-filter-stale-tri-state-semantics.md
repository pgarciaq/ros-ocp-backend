# ADR-0225: filter[stale] tri-state semantics

## Status

Accepted

## Context

API consumers need to filter recommendations by staleness. A boolean filter is insufficient — they need "exclude stale" (default), "include all", or "only stale."

## Decision

`filter[stale]` has three values:

1. Absent or `false` — exclude stale rows (default for all list endpoints).
2. `true` — include both stale and non-stale.
3. `only` — return only stale rows.

Applied via `applyRecommendationStaleFilter`.

## Alternatives Considered

### Boolean only

Cannot query "only stale" workloads.

### Separate endpoint for stale recommendations

Duplication of list logic and pagination.

## Consequences

- `filter[stale]=true` does NOT mean "only stale" — it means "include stale in results."
- Non-obvious semantics differ from typical boolean filter behavior.
- Fleet/savings SQL hardcodes `stale = false`.

## Related Decisions

- [ADR-0224](0224-stale-marking-precedence-last-reported-at-overrides-digest-age.md): Staleness precedence.
- [ADR-0161](0161-staleness-threshold-hours-alias.md): Staleness threshold.

## References

- [internal/api/filters_stale.go](../../internal/api/filters_stale.go)
