# ADR-0052: Use org_container_keys denormalized index for list pagination

## Status

Accepted

## Context

Scanning full recommendation_sets (200k+ rows) for pagination was too slow.

## Decision

Maintain denormalized `org_container_keys` table optimized for list/filter/sort.

## Consequences

Fast pagination. Requires sync on ingest. Extra storage for denormalized data.

## References

- [internal/model/native_list_keys.go](internal/model/native_list_keys.go)
- [internal/model/org_container_keys.go](internal/model/org_container_keys.go)
