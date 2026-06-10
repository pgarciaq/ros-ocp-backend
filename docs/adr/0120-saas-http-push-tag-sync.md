# ADR-0120: Use SaaS HTTP push full-replace sync

## Status

Accepted

## Context

Incremental patch complexity not justified for Celery-driven periodic updates.

## Decision

Full-replace sync to `org_container_keys.resolved_tags` on each push.

## Consequences

Simple. Idempotent. Brief inconsistency window during replace. Acceptable for tag freshness.

## References

- [internal/tags/sync.go](internal/tags/sync.go)
