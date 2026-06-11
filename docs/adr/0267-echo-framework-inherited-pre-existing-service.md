# ADR-0267: Echo framework inherited from pre-existing service

## Status

Accepted

> **Note:** This ADR documents a straightforward or inherited decision kept for completeness and historical traceability. It does not represent a non-obvious architectural fork.

## Phase

Pre-0

## Context

Echo v4 was already in use for the REST API when native engine work began. The service had middleware, route registration, and handler patterns built around Echo's context model.

No compelling performance or architectural reason to migrate mid-project.

## Decision

Retain Echo v4 for HTTP routing and middleware. Accept its middleware chain model, context pattern, and route registration semantics.

## Alternatives Considered

### Gin

Slightly faster benchmarks; different middleware model requires rewrite.

### Chi

Stdlib-compatible router; more boilerplate for middleware chain.

### stdlib net/http only

No framework dependency but significant boilerplate increase.

## Consequences

- Route registration order matters ([ADR-0192](0192-route-registration-order-specific-before-catchall.md)).
- Echo-specific middleware patterns throughout handlers.
- `echo.Context` threading through all API handlers.
- Upgrade to Echo v5 would require migration effort.

## Related Decisions

- [ADR-0192](0192-route-registration-order-specific-before-catchall.md): Route registration order.
- [ADR-0168](0168-disabled-plugin-route-guards-before-catchall.md): Catch-all guards.
- [ADR-0207](0207-stdlib-json-encoding-over-echo-default.md): Stdlib JSON encoding.
- [ADR-0244](0244-request-correlation-echo-request-id.md): Echo request_id correlation.

## References

- [internal/api/server.go](../../internal/api/server.go)
- [internal/api/routes.go](../../internal/api/routes.go)
