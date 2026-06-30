# ADR-0151: Use RBAC fail-closed with in-memory cache (60s TTL)

## Status

Accepted

## Context

Per-row RBAC calls on paginated lists would be prohibitively slow.

## Decision

Cache RBAC decisions 60s; fail-closed (deny) on RBAC service unavailability.

## Alternatives Considered

### Per-request RBAC with no cache
Calling the RBAC service for every row in a paginated list (50–100 items) adds 50–100 HTTP round-trips per API call, pushing p99 list latency past acceptable UI thresholds even when RBAC itself is healthy.

### Fail-open on RBAC outage
Allowing access when RBAC is unreachable keeps the UI functional during incidents, but violates the security posture expected by enterprise reviewers—a compromised or partitioned RBAC service would grant full org visibility instead of denying by default.

### Long-lived cache (15+ minutes)
Extended TTL reduces RBAC load further, but permission revocations (removed cluster access, role demotions) would remain visible in ROS for too long; 60s balances freshness against the fact that RBAC changes are rare compared to read traffic.

## Consequences

Fast pagination. Brief stale permissions (60s). Deny-by-default on failure.

## Implementation Details

### Bounded LRU cache

RBAC permission results are stored in a bounded LRU cache keyed by hashed `X-Rh-Identity` (`internal/api/middleware/rbac_cache.go`).

- **`ROS_RBAC_CACHE_MAX_ENTRIES`** (default **500**) caps memory; oldest entries evict when full.
- TTL-on-access preserved at 60 seconds (`ROS_RBAC_CACHE_TTL`).
- Prometheus metrics: `rosocp_rbac_cache_size` (gauge), `rosocp_rbac_cache_removals_total` (counter).

### Pagination cap (DoS mitigation)

RBAC ACL enumeration uses iterative pagination with **`maxRBACPages = 50`** in `internal/api/middleware/rbac.go`. This prevents a malicious or misconfigured RBAC service from forcing unbounded permission enumeration per API request (full-org permission list DoS).

## References

- [internal/api/middleware/rbac_cache.go](../../internal/api/middleware/rbac_cache.go)
- [internal/api/middleware/rbac.go](../../internal/api/middleware/rbac.go)
