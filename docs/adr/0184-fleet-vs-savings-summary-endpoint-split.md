# ADR-0184: Fleet-summary vs savings-summary endpoint split

## Status

Accepted

## Context

The dashboard needs both a fast fleet overview ("how many containers and quick savings total") and a detailed savings breakdown by resource type, term, and engine. These queries have different costs, caching profiles, and exclusion rules ([ADR-0071](0071-exclude-gpu-from-savings-summary.md), [ADR-0072](0072-exclude-quota-from-fleet-savings.md)).

## Decision

Maintain two separate endpoints:

| Endpoint | Purpose | Constraints |
|----------|---------|-------------|
| `/fleet-summary` | Container counts + container-only savings | Fixed `term=medium`, `engine=cost`; RBAC-scoped LRU cache ([ADR-0185](0185-fleet-savings-lru-cache-rbac-keys.md)) |
| `/savings-summary` | Multi-plugin persisted savings breakdown | Configurable term/engine; excludes GPU fleet total and quota; supports `group_by[idle_state\|tag]`; GPU dollars computed at read time with explanatory note |

No single unified "total savings" endpoint exists by design—resource types compute savings differently.

## Alternatives Considered

### Single unified endpoint

Too slow for dashboard refresh when including full multi-plugin breakdown.

### Client-side aggregation from list endpoints

Pushes complexity and bandwidth cost to the frontend.

## Consequences

- Dashboards must call both endpoints for complete overview + breakdown.
- Fleet summary is fast (cached); savings summary is slower but configurable.
- Idle waste totals appear in savings summary via `group_by[idle_state]`, not fleet savings total.

## Related Decisions

- [ADR-0071](0071-exclude-gpu-from-savings-summary.md): GPU savings exclusion from fleet total.
- [ADR-0072](0072-exclude-quota-from-fleet-savings.md): Quota savings exclusion.
- [ADR-0112](0112-bounded-lru-ttl-cost-cache.md): LRU cache pattern.

## References

- [internal/api/handlers_fleet.go](../../internal/api/handlers_fleet.go)
- [internal/api/handlers_savings_summary.go](../../internal/api/handlers_savings_summary.go)
