# ADR-0048: Use relational columns on recommendation_sets, not JSONB blobs

## Status

Accepted

## Context

JSONB prevented indexing, filtering, and stable API contracts.

## Decision

Typed columns for all recommendation fields. JSONB reserved for non-query metadata only.

## Consequences

Full SQL filtering. Schema evolution via migrations. More columns but better performance.

## References

- [migrations/000026](migrations/000026)
- [internal/model/recommendation_set_native.go](internal/model/recommendation_set_native.go)
