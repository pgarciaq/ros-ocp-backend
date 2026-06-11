# ADR-0119: Use on-prem DB join to Koku tag tables (ROS_TAGS_SOURCE=db)

## Status

Accepted

## Context

On-prem shares PostgreSQL with Koku; HTTP sync adds unnecessary complexity.

## Decision

Default on-prem: JOIN Koku tag tables directly.

## Alternatives Considered

### HTTP sync to Koku tags API on-prem
Pulling tags over HTTP decouples schemas, but on-prem ROS and Koku share one PostgreSQL instance—HTTP adds serialization overhead, requires network routing between co-located pods, and duplicates data already in `reporting_ocptags*` tables.

### Read-through cache with TTL
Caching tag lookups in Redis reduces JOIN cost, but tag enable/disable in Koku settings would appear stale until TTL expiry; a SQL JOIN in `db_provider.go` reflects tag state at query time with zero sync lag.

### Denormalized tag column on org_container_keys
Storing resolved tags on every list row avoids runtime joins, but every tag key enable/disable in Koku triggers a full-table rewrite in ROS—exactly the sync problem ADR-0120 solves for SaaS via HTTP push instead.

## Consequences

Zero-latency tag access. Tight coupling to Koku schema. On-prem only.

## References

- [internal/tags/db_provider.go](internal/tags/db_provider.go)
