# ADR-0119: Use on-prem DB join to Koku tag tables (ROS_TAGS_SOURCE=db)

## Status

Accepted

## Context

On-prem shares PostgreSQL with Koku; HTTP sync adds unnecessary complexity.

## Decision

Default on-prem: JOIN Koku tag tables directly.

## Consequences

Zero-latency tag access. Tight coupling to Koku schema. On-prem only.

## References

- [internal/tags/db_provider.go](internal/tags/db_provider.go)
