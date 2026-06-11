# ADR-0218: Org-level reship single-flight with trailing batch coalescing

## Status

Accepted

## Context

Multiple business hours setting changes in quick succession should not trigger redundant reshipping. The reship guard operates at org level (not cluster level) because a single org BH change can affect multiple clusters.

## Decision

`trigger_guard.go` coalesces reship by `org_id`. The pending cluster UUID list is replaced (not appended) while in-flight — latest parameters win. Fleet cache is invalidated after batch completes.

This is the outer coalescing layer; `reship.Service` has per-cluster locking internally.

## Alternatives Considered

### Per-cluster only

No batch efficiency; duplicate Masu calls for concurrent org edits.

### Global single-flight

Blocks unrelated orgs unnecessarily.

## Consequences

- Two layers of concurrency control (org-batch + per-cluster).
- Only the latest cluster list is used when coalescing occurs.
- Debugging "reship ran twice" requires understanding both layers.

## Related Decisions

- [ADR-0125](0125-single-flight-trailing-reship.md): Trailing-params pattern.
- [ADR-0216](0216-business-hours-pending-marker-stub-rows.md): Pending markers.
- [ADR-0219](0219-reship-background-poller-retries-pending-clusters.md): Background poller retries.

## References

- [internal/reship/trigger_guard.go](../../internal/reship/trigger_guard.go)
- [internal/reship/service.go](../../internal/reship/service.go)
