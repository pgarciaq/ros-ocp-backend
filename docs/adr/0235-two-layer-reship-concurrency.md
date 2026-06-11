# ADR-0235: Two-layer reship concurrency — org coalescing plus per-cluster advisory lock

## Status

Accepted

## Context

Business hours reship rebuilds digest streams from S3 via Koku Masu ([ADR-0124](0124-koku-reship-ros-rebuild-bh.md)). Concurrent triggers arise from rapid BH settings edits ([ADR-0220](0220-bh-put-triggers-reship-threshold-put-triggers-recalc.md)) and background poller retries ([ADR-0219](0219-reship-background-poller-retries-pending-clusters.md)).

[ADR-0218](0218-org-level-reship-single-flight-trailing-batch-coalescing.md) coalesces at org level. Without an inner lock, two org-batch workers could reship the same cluster concurrently, causing duplicate Masu calls and digest races.

## Decision

Use **two layers** of concurrency control:

1. **Outer (org):** `trigger_guard.go` coalesces reship triggers by `org_id`. Pending cluster UUID lists are replaced while in-flight; latest parameters win ([ADR-0218](0218-org-level-reship-single-flight-trailing-batch-coalescing.md)).
2. **Inner (cluster):** `reship.Service.TriggerReship` acquires a per-cluster `LockCoordinator` advisory lock before Masu reship for that cluster.

If the BH schedule changes while a cluster reship is in-flight, a **trailing second pass** runs after lock release when the schedule version differs ([ADR-0125](0125-single-flight-trailing-reship.md)).

## Alternatives Considered

### Org lock only

Duplicate work and race on digest writes for hot clusters under org-wide BH edits.

### Cluster lock only

No batch efficiency; thundering herd on multi-cluster org updates.

### Database row lock on cluster

Heavier than in-process coordinator; cross-process reship is single-worker per deployment today.

## Consequences

- Debugging "reship ran twice" requires checking org coalescing vs trailing second pass vs poller retry.
- Metrics should distinguish coalesced org triggers from trailing cluster passes.
- Per-cluster lock scope bounds Masu parallelism to one reship per cluster at a time.

## Related Decisions

- [ADR-0218](0218-org-level-reship-single-flight-trailing-batch-coalescing.md): Org-level coalescing.
- [ADR-0125](0125-single-flight-trailing-reship.md): Trailing reship on schedule change.
- [ADR-0219](0219-reship-background-poller-retries-pending-clusters.md): Background poller retries.

## References

- [internal/reship/trigger_guard.go](../../internal/reship/trigger_guard.go)
- [internal/reship/service.go](../../internal/reship/service.go)
- [internal/reship/lock_coordinator.go](../../internal/reship/lock_coordinator.go)
