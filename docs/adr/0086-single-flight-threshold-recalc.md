# ADR-0086: Use single-flight coalescing per (org_id, recommendation_type) on recalc

## Status

Accepted

## Context

Admins spamming threshold PUTs could spawn unbounded goroutines.

## Decision

`golang.org/x/sync/singleflight` keyed by `(org_id, recType)`.

## Alternatives Considered

### Debounce-only (time-based coalescing)
A fixed debounce window (e.g., 5s after last PUT) would reduce goroutine storms without tracking in-flight state, but admins changing multiple recommendation types in sequence would still spawn parallel recalcs, and trailing updates within the window could be dropped instead of coalesced into a follow-up run.

### Celery/Redis job queue with deduplication keys
An external queue would cap concurrency cluster-wide and survive process restarts, but cost-onprem already runs Valkey for Koku—not ROS—and adding queue infrastructure for a rare admin action (threshold PUT) violates the single-binary deployment model.

### Mutex per org blocking all recalc types
A single org-wide lock is simpler than `(org_id, recType)` scoping, but PVC threshold changes would block unrelated container recalcs for the same tenant, increasing perceived latency on unrelated recommendation types.

## Consequences

At most one recalc in-flight per scope. Subsequent requests coalesced. Metric for coalesced count.

## References

- [internal/engine/threshold_recalc_guard.go](internal/engine/threshold_recalc_guard.go)
