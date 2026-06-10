# ADR-0086: Use single-flight coalescing per (org_id, recommendation_type) on recalc

## Status

Accepted

## Context

Admins spamming threshold PUTs could spawn unbounded goroutines.

## Decision

`golang.org/x/sync/singleflight` keyed by `(org_id, recType)`.

## Consequences

At most one recalc in-flight per scope. Subsequent requests coalesced. Metric for coalesced count.

## References

- [internal/engine/threshold_recalc_guard.go](internal/engine/threshold_recalc_guard.go)
