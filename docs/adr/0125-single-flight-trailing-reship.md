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

Post-completion `fleetsummary.InvalidateOrg(orgID)` is called in `trigger_guard.go` after each coalesced reship batch. Reship invalidates at completion only (no pre-trigger), unlike savings and threshold recalc which use invalidate-twice ([ADR-0118](0118-invalidate-cost-cache-on-settings-change.md)).

## References

- [internal/reship/service.go](internal/reship/service.go)
- [internal/reship/trigger_guard.go](internal/reship/trigger_guard.go)

## Update (2026-11-15, #812 — savings guard granularity; decision text above immutable)

This ADR's pattern was borrowed by the savings-recalculation guard
(`internal/engine/savings_recalc_guard.go`), whose triggers carry
**per-cluster identity** — unlike reship schedule edits, whose latest
parameters are the org's current state. The single latest-slot therefore
dropped overlapping per-cluster callbacks (flight running for A; B then C
arrive; only C survives), and koku's per-provider callbacks for providers
sharing a price list make that overlap reachable. The savings guard's
trailing state is now a **set of pending clusters** keyed by cluster UUID
(same-cluster arrivals union their recTypes; an org-wide request coexists
with specific-cluster requests — no subsumption). The drain loop pops one
entry at a time and re-checks after each recalculation, so arrivals during
a trailing run are picked up. The reship guard itself keeps this ADR's
single latest-batch semantics: its arrivals are resolved from current
settings state, and `MarkReshipPending` plus the poller re-drive any
cluster whose batch was displaced.
