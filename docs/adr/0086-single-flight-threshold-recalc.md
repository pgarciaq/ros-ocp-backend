# ADR-0086: Use single-flight coalescing per (org_id, recommendation_type) on recalc

## Status

Accepted

## Context

Admins spamming threshold PUTs could spawn unbounded goroutines.

## Decision

Custom `recalcFlight` struct in `threshold_recalc_guard.go` keyed by `(org_id, recType)`:

- If a recalc is already running, set `pending` and increment `rosocp_threshold_recalc_coalesced_total`.
- When the in-flight run completes, if `pending` was set, run again with the same parameters (trailing pass).
- Uses the same trailing latest-params loop pattern as savings and reship guards ([ADR-0125](0125-single-flight-trailing-reship.md)).

Post-completion, `fleetsummary.InvalidateOrg(orgID)` runs as part of the invalidate-twice pattern ([ADR-0118](0118-invalidate-cost-cache-on-settings-change.md)); pre-trigger invalidation occurs in `TriggerThresholdRecalculationAsync`.

## Alternatives Considered

### Debounce-only (time-based coalescing)
A fixed debounce window (e.g., 5s after last PUT) would reduce goroutine storms without tracking in-flight state, but admins changing multiple recommendation types in sequence would still spawn parallel recalcs, and trailing updates within the window could be dropped instead of coalesced into a follow-up run.

### Celery/Redis job queue with deduplication keys
An external queue would cap concurrency cluster-wide and survive process restarts, but cost-onprem already runs Valkey for Koku—not ROS—and adding queue infrastructure for a rare admin action (threshold PUT) violates the single-binary deployment model.

### Mutex per org blocking all recalc types
A single org-wide lock is simpler than `(org_id, recType)` scoping, but PVC threshold changes would block unrelated container recalcs for the same tenant, increasing perceived latency on unrelated recommendation types.

## Consequences

At most one recalc in-flight per scope. Subsequent requests coalesced into a trailing pass. Metric for coalesced count. Fleet/savings caches invalidated before and after recalc.

## Related Decisions

- [ADR-0125](0125-single-flight-trailing-reship.md): Same coalescing pattern for reship and savings recalc.
- [ADR-0118](0118-invalidate-cost-cache-on-settings-change.md): Invalidate-twice contract for async recalc.

## References

- [internal/engine/threshold_recalc_guard.go](internal/engine/threshold_recalc_guard.go)
