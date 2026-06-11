# ADR-0120: Use SaaS HTTP push full-replace sync

## Status

Accepted

## Context

Incremental patch complexity not justified for Celery-driven periodic updates.

## Decision

Full-replace sync to `org_container_keys.resolved_tags` on each push.

## Alternatives Considered

### Incremental patch (add/remove tag keys per container)
Delta updates minimize payload size, but ordering guarantees (remove before add), idempotency on duplicate pushes, and tombstone handling for deleted tags add complexity disproportionate to Celery's periodic full-tag exports from Koku.

### Pull-from-Koku polling in ROS
Having ROS poll Koku's tags API on a schedule avoids push infrastructure, but wastes HTTP round-trips when tags haven't changed and introduces polling latency between Koku settings updates and list filter availability.

### DB join in SaaS (same as on-prem)
SaaS ROS and Koku run in separate schemas/services without shared PostgreSQL—cross-schema JOINs would couple deployment topology and break when ROS scales to dedicated RDS instances independent of Koku's tenant DB.

## Consequences

Simple. Idempotent. Brief inconsistency window during replace. Acceptable for tag freshness.

## References

- [internal/tags/sync.go](internal/tags/sync.go)
