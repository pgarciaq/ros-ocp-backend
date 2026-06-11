# ADR-0205: RBAC filter intersection semantics

## Status

Accepted

> **Note:** This ADR documents a straightforward or inherited decision kept for completeness and historical traceability. It does not represent a non-obvious architectural fork.

## Context

Users may have RBAC permissions scoped to specific clusters AND specific projects. The system must determine how to combine these scopes when filtering recommendations.

## Decision

When the user has both cluster and project permissions (neither is wildcard `*`), filters apply **both as intersection** (`cluster_uuid IN … AND namespace IN …`).

**Node RBAC:** Uses `openshift.node` resource against `gpu_container_digests.node_name` for GPU endpoints.

**Global bypass:** `*` permission bypasses all filters.

**Fail-closed:** 403 if RBAC service returns nil permissions ([ADR-0151](0151-rbac-fail-closed-cache-60s.md)).

## Consequences

- Users see only recommendations matching **all** permission scopes (most restrictive).
- Differs from systems that use "most permissive" union — more secure but potentially confusing if users expect to see clusters they have partial access to.

## Alternatives Considered

### Union (most permissive)

Security risk — shows data outside scope. Rejected.

### Per-endpoint scope selection

Inconsistent UX across endpoints. Rejected.

### No RBAC at application level

Trusts gateway only — insufficient defense-in-depth. Rejected.

## Related Decisions

- [ADR-0151](0151-rbac-fail-closed-cache-60s.md): RBAC fail-closed with cache.
- [ADR-0167](0167-cost-management-entitlement-middleware.md): Entitlement middleware.

## References

- [internal/rbac/query_builder.go](../../internal/rbac/query_builder.go)
- [internal/api/middleware/rbac.go](../../internal/api/middleware/rbac.go)
