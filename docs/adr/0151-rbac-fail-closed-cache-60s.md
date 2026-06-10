# ADR-0151: Use RBAC fail-closed with in-memory cache (60s TTL)

## Status

Accepted

## Context

Per-row RBAC calls on paginated lists would be prohibitively slow.

## Decision

Cache RBAC decisions 60s; fail-closed (deny) on RBAC service unavailability.

## Consequences

Fast pagination. Brief stale permissions (60s). Deny-by-default on failure.

## References

- [internal/api/middleware/rbac_cache.go](internal/api/middleware/rbac_cache.go)
