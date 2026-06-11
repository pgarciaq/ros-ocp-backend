# ADR-0283: Synchronous REST API — no WebSocket/SSE for recommendation updates

## Status

Accepted

## Phase

Pre-0 through 12

## Context

Recommendations update on ingest (every upload cycle, typically hourly). Recalculation takes seconds. No real-time streaming requirement was identified across 12 development phases.

WebSocket infrastructure adds connection management complexity for marginal UX benefit.

## Decision

Poll-based REST API only. No WebSocket or Server-Sent Events for recommendation updates. Recalculation returns 202 Accepted; clients poll for results. Dashboard refresh on page load is sufficient given hourly update cadence.

## Alternatives Considered

### WebSocket push

Connection management complexity and infrastructure requirements.

### Server-Sent Events

Simpler than WebSocket but still requires long-lived connections.

### Webhook callback

Requires client-side server infrastructure.

## Consequences

- Simple infrastructure (no WebSocket connection management).
- Clients handle their own refresh timing.
- Long recalculations (minutes for large orgs) require polling with backoff.
- Future consideration if sub-second updates become a requirement.

## Related Decisions

- [ADR-0082](0082-recalculate-savings-async-202.md): Async 202 recalc pattern.
- [ADR-0185](0185-list-response-cache-headers.md): List response caching.
- [ADR-0270](0270-on-demand-api-time-recommendations-deferred.md): Realtime recs deferred.

## References

- [internal/api/handlers_recalc.go](../../internal/api/handlers_recalc.go)
- [openapi/openapi.yaml](../../openapi/openapi.yaml)
