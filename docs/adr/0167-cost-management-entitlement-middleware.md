# ADR-0167: Cost-management entitlement middleware (defense-in-depth)

## Status

Accepted

## Context

The gateway (Envoy/3scale) validates JWT tokens and entitlements before traffic reaches ROS-OCP. Defense-in-depth requires the application to verify entitlements independently because:

1. Gateway misconfiguration could bypass checks.
2. Internal routing may skip the gateway.
3. On-prem deployments synthesize identity headers with hardcoded entitlements.

## Decision

`CostManagementEntitlement` middleware in `internal/api/middleware/entitlement.go` checks `entitlements.cost_management.is_entitled == true` in the decoded `x-rh-identity` header for all v1 API routes. Returns **403** if not entitled.

- Skipped when `DEVELOPMENT=true` for local UX.
- Internal endpoints (`/internal/*`) are exempt—they use service-account auth, not user identity.

## Alternatives Considered

### Trust gateway only

Single point of failure; a misconfigured or bypassed gateway grants full API access without product entitlement.

### Check at database layer

Too late—query planning and RBAC work already consumed resources before entitlement could be enforced.

### RBAC-only

RBAC checks access scope within an entitled product; it does not verify `cost_management.is_entitled`.

## Consequences

- Defense-in-depth: even if the gateway fails, unauthorized users receive 403.
- Adds ~100μs per request (JSON field access on already-decoded identity).
- On-prem Envoy Lua filter hardcodes `is_entitled: true`, so middleware is always satisfied there.

## Related Decisions

- [ADR-0149](0149-block-dev-token-outside-development.md): Dev-token blocking (distinct security control).
- [ADR-0150](0150-validate-sa-allowlist-at-startup.md): SA allowlist for internal endpoints.

## References

- [internal/api/middleware/entitlement.go](../../internal/api/middleware/entitlement.go)
- [internal/api/server.go](../../internal/api/server.go) — v1 group registration
