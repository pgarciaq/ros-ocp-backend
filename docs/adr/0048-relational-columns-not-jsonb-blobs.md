# ADR-0048: Use relational columns on recommendation_sets, not JSONB blobs

## Status

Accepted

## Context

JSONB prevented indexing, filtering, and stable API contracts.

## Decision

Typed columns for all recommendation fields. JSONB reserved for non-query metadata only.

## Alternatives Considered

### JSONB document blobs with GIN indexes
Kruize stored recommendations as JSONB with GIN indexing, but the PostgreSQL planner cannot push predicates on nested paths into index scans reliably—filtering by notification code or cost range devolved to sequential scans over 200k+ rows per org.

### Schemaless document model (one JSON column per entity)
A fully schemaless approach accelerates early prototyping, but lacks compile-time type safety in Go, prevents `CHECK` constraints on millicore/memory bounds, and makes additive API fields a manual migration pain across every consumer.

### Hybrid: hot columns + JSONB overflow
Keeping frequently filtered fields relational and stuffing the rest in JSONB seemed a compromise, but list/sort paths still hit JSONB for savings and notification arrays; the native engine migrated all query-facing fields to typed columns in migration `000026` for uniform index coverage.

## Consequences

Full SQL filtering. Schema evolution via migrations. More columns but better performance.

## References

- [migrations/000026](migrations/000026)
- [internal/model/recommendation_set_native.go](internal/model/recommendation_set_native.go)
