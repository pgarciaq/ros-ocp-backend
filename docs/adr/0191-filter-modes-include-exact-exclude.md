# ADR-0191: Filter modes — include (ILIKE), exact, and exclude with different SQL semantics

## Status

Accepted

## Context

API consumers need flexible filtering: "show me namespaces containing 'prod'" (include), "show exactly namespace 'prod-west'" (exact), "hide DaemonSets" (exclude). Each mode has different SQL generation requirements. A single filter mode cannot express all real query patterns.

## Decision

Three filter modes with distinct SQL semantics:

| Mode | SQL | Multi-value logic |
|------|-----|-------------------|
| **include** | `ILIKE` with `%value%` | OR (any match) |
| **exact** | `=` | OR (any match) |
| **exclude** | `!= ALL(...)` | AND (all must not match) |

ILIKE values are escaped per [ADR-0147](0147-escape-ilike-wildcards.md). Workload types are a closed enum validated against DB enum values. New filters must declare their mode and be added to the query allowlist ([ADR-0169](0169-allowlisted-native-sql-query-fragments.md)).

Include and exact cannot be combined on the same field in a single request.

## Consequences

- Adding a new filter requires declaring its mode and adding to the allowlist.
- Multi-value include/exact are OR'd (any match); multi-value exclude are AND'd (all must not match).
- Cannot combine include and exact on the same field in a single request.

## Alternatives Considered

### Single filter mode

Insufficient for real use cases (substring search vs exact match vs negation). Rejected.

### GraphQL-style filtering

Flexible but over-engineering for a REST API with a fixed allowlist. Rejected.

## Related Decisions

- [ADR-0147](0147-escape-ilike-wildcards.md): ILIKE wildcard escaping.
- [ADR-0169](0169-allowlisted-native-sql-query-fragments.md): Query fragment allowlist.

## References

- [internal/api/common.go](../../internal/api/common.go)
- [internal/model/native_query_allowlist.go](../../internal/model/native_query_allowlist.go)
