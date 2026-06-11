# ADR-0224: Stale marking precedence — last_reported_at overrides digest age

## Status

Accepted

## Context

A recommendation becomes stale when its underlying data is too old. During reship (historical backfill), recent data arrives with old bucket dates. Staleness should reflect cluster reporting health, not digest bucket age alone.

## Decision

`isStaleRecommendation`: if `clusters.last_reported_at` is within the staleness window, the recommendation is NOT stale regardless of digest bucket date.

This supports reship scenarios where historical buckets arrive from an actively-reporting cluster.

## Alternatives Considered

### Bucket age only

Incorrectly marks reshipped data stale while cluster is healthy.

### Never stale during reship

Requires explicit reship-in-progress flag on every cluster.

## Consequences

- [ADR-0161](0161-staleness-threshold-hours-alias.md) documents the env alias but not this precedence rule.
- A cluster that recently reported keeps all its recommendations non-stale even if individual digests are old.
- Staleness detection runs during produce, not at query time.

## Related Decisions

- [ADR-0161](0161-staleness-threshold-hours-alias.md): Staleness threshold env alias.
- [ADR-0124](0124-koku-reship-ros-rebuild-bh.md): Reship backfill.
- [ADR-0225](0225-filter-stale-tri-state-semantics.md): API stale filter.

## References

- [internal/engine/staleness.go](../../internal/engine/staleness.go)
