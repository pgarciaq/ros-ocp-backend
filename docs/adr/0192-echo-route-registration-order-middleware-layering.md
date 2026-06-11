# ADR-0192: Echo route registration order and middleware layering

## Status

Accepted

## Context

The Echo framework matches routes in registration order. The API has unauthenticated routes (notification-codes, health), SA-authenticated internal routes, and user-authenticated v1 routes with RBAC. A catch-all `/:recommendation-id` must be registered last or it intercepts valid paths and surfaces UUID parse failures.

## Decision

Registration order in `internal/api/server.go`:

1. **Health/readyz** — no auth
2. **Notification-codes** — no auth
3. **Internal routes** — SA auth only, no RBAC/identity middleware
4. **v1 routes** — identity + entitlement + RBAC middleware
5. **Disabled plugin guards** ([ADR-0168](0168-disabled-plugin-route-guards.md))
6. **`/:recommendation-id` catch-all** — last

Settings writes require `cost-management:settings:write` RBAC permission.

## Consequences

- Adding new routes requires understanding middleware layering.
- Internal routes skip identity middleware entirely (they use SA tokens).
- Misplaced routes surface as auth errors or UUID parse failures.

## Alternatives Considered

### Sub-routers per auth level

Echo does not support nested groups cleanly for this middleware stack. Rejected.

### Middleware checks route metadata

Less explicit than registration order; harder to audit which routes require which auth. Rejected.

## Related Decisions

- [ADR-0168](0168-disabled-plugin-route-guards.md): Disabled plugin route guards before catch-all.
- [ADR-0167](0167-cost-management-entitlement-middleware.md): Entitlement middleware on v1 routes.

## References

- [internal/api/server.go](../../internal/api/server.go)
