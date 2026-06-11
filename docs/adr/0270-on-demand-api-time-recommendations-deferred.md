# ADR-0270: On-demand API-time recommendations deferred (ROS_ENABLE_REALTIME_RECS)

## Status

Accepted

## Phase

1–3

## Context

Requirements included custom window recommendations at API time — user requests P95 over 12 days, engine computes on the fly. Feature flag `ROS_ENABLE_REALTIME_RECS` was planned in phase 1 design docs.

On-demand computation adds latency to GET requests and requires digest access at the API layer.

## Decision

Defer indefinitely. Ship pre-computed terms only (1d/7d/15d + per-org overrides via settings). On-demand computation adds latency to GET requests (1–5ms per container × page size), requires digest access at API layer, and complicates caching.

Feature flag never implemented.

## Alternatives Considered

### Implement realtime at API layer

Latency on reads; cache invalidation complexity.

### Background pre-compute on demand

Queue + polling UX; over-engineering for unvalidated demand.

## Consequences

- API always returns pre-computed recommendations.
- Custom windows require settings change + async recalc (seconds, not milliseconds).
- Simpler caching model ([ADR-0185](0185-list-response-cache-headers.md)).
- `ROS_ENABLE_REALTIME_RECS` env var never added.

## Related Decisions

- [ADR-0261](0261-three-terms-short-medium-long-kruize-aligned-defaults.md): Three terms architecture.
- [ADR-0085](0085-threshold-cache-ttl-60s-async-recalc.md): Async recalc on settings change.
- [ADR-0003](0003-read-once-compute-n-terms.md): Read once, compute N terms.

## References

- [docs/archive/phase1/requirements.md](../../docs/archive/phase1/requirements.md)
- [internal/engine/produce.go](../../internal/engine/produce.go)
