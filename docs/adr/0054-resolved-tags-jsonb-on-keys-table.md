# ADR-0054: Store resolved_tags JSONB on keys table

## Status

Accepted

## Context

Normalizing every tag key/value row would explode join complexity.

## Decision

Store resolved tags as JSONB on `org_container_keys` for read-whole metadata access.

## Consequences

Fast tag reads. JSONB for metadata is acceptable (not queried with SQL operators). Tags updated via sync.

## References

- [internal/tags/sync.go](internal/tags/sync.go)
