# ADR-0152: Cap history filter param cardinality (5 values per param)

## Status

Accepted

## Context

Unbounded IN clauses from query-string explosion enable DoS.

## Decision

Maximum 5 values per filter parameter on history endpoints.

## Consequences

Bounded query complexity. May limit advanced filtering use cases.

## References

- [internal/api/handlers_history.go](internal/api/handlers_history.go)
