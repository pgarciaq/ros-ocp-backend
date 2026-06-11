# ADR-0186: Per-cluster threshold hash skip for recalculation efficiency

## Status

Accepted

## Context

When settings change (e.g., idle thresholds), async recalculation runs across all clusters ([ADR-0085](0085-threshold-cache-ttl-60s-async-recalc.md), [ADR-0086](0086-single-flight-threshold-recalc.md)). Many clusters may already have identical resolved settings—recalculating them wastes CPU and extends time-to-consistency.

## Decision

After settings PUT, async recalc:

1. Computes SHA-256 of resolved settings per `recommendation_type`
2. Skips clusters whose stored hash in `cluster_threshold_recalc_state` matches
3. Stores updated hash per cluster per recommendation type after successful recalc
4. Runs with default concurrency of 3 clusters (`ROS_THRESHOLD_RECALC_CONCURRENCY`)

Adding new settings fields changes the hash and forces one-time recalculation of all clusters.

## Alternatives Considered

### Always recalculate all clusters

Wasteful at fleet scale for idempotent or no-op settings updates.

### Timestamp-based skip

Does not detect idempotent re-PUT of identical settings values.

### Per-field change detection

Brittle when new settings fields are added; requires manual invalidation wiring.

## Consequences

- No-op settings changes do not trigger engine runs on already-matched clusters.
- Clusters with cluster-level overrides get different hashes and recalculate when org settings change.
- Hash schema changes force a one-time full recalc—acceptable cost on deploy.

## Related Decisions

- [ADR-0086](0086-single-flight-threshold-recalc.md): Single-flight coalescing on recalc.
- [ADR-0085](0085-threshold-cache-ttl-60s-async-recalc.md): Async recalc on settings PUT.

## References

- [internal/engine/threshold_recalc_state.go](../../internal/engine/threshold_recalc_state.go)
- [internal/engine/threshold_recalculate.go](../../internal/engine/threshold_recalculate.go)
- [migrations/000077_cluster_threshold_recalc_state.up.sql](../../migrations/000077_cluster_threshold_recalc_state.up.sql)
