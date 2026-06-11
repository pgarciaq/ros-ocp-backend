# ADR-0125: Use single-flight lock + trailing reship on concurrent schedule edits

## Status

Accepted

## Context

Parallel reships from rapid schedule changes duplicate Kafka load.

## Decision

Single-flight lock; trailing pass after in-flight completes—coalesces rapid-fire schedule edits into one reship after cooldown.

## Alternatives Considered

### Queue-based reship
Adds Kafka or Redis dependency for a problem solvable in-process; more moving parts for on-prem minimal stacks.

### Immediate parallel reship per edit
Thundering herd on Koku listener when operators save BH schedules repeatedly; duplicate Kafka messages rebuild the same historical window.

## Related Decisions

- [ADR-0124](0124-koku-reship-ros-rebuild-bh.md): initial reship trigger when BH settings change.
- [ADR-0126](0126-forward-only-fallback-reship-failure.md): forward-only fallback when trailing reship exhausts retries.

## Consequences

At most one reship in-flight. Latest schedule always applied via trailing pass. Alert on repeated reship failures (`rosocp_reship_*` metrics) when trailing coalescing cannot complete.

## References

- [internal/reship/service.go](internal/reship/service.go)
