# ADR-0169: Allowlisted native SQL query fragments

## Status

Accepted

## Context

`ApplyQueryParams` in the model layer builds WHERE clauses from handler-supplied filter keys. If arbitrary strings could be passed as column names or operators, it would enable SQL injection through the ORM. GORM parameterizes values but not column identifiers.

## Decision

`internal/model/native_query_allowlist.go` defines a compile-time allowlist of permitted query keys (column names, operators, join paths). `ApplyQueryParams` rejects any key not in the allowlist. Each handler declares its allowed filters; the allowlist is the union. Integration tests verify handler-declared filters are a subset of the allowlist.

## Alternatives Considered

### ORM parameterization only

Protects bind values but not column identifiers or SQL fragment structure.

### Runtime reflection on model struct tags

Fragile, hard to audit, and easy to miss handler-specific join aliases (`rs.`, `q.`, `h.`).

### Per-handler hardcoded queries

Duplicates filtering logic across handlers and diverges from shared `ApplyQueryParams` behavior.

## Consequences

- Prevents filter injection even if handler code is careless.
- Maintenance burden: adding a new filterable column requires updating the allowlist and handler comments.
- Compile-time enforcement (not runtime discovery).
- Distinct from [ADR-0147](0147-escape-ilike-wildcards.md) (ILIKE value escape) which handles value sanitization.

## Related Decisions

- [ADR-0057](0057-allowlisted-bucket-sql-expressions.md): Allowlisted bucket SQL expressions for time-series queries.
- [ADR-0147](0147-escape-ilike-wildcards.md): ILIKE wildcard escaping in filter values.

## References

- [internal/model/native_query_allowlist.go](../../internal/model/native_query_allowlist.go)
- [internal/model/native_query_allowlist_test.go](../../internal/model/native_query_allowlist_test.go)
- [internal/api/handlers_history.go](../../internal/api/handlers_history.go) — handler/allowlist sync comments
- [internal/api/handlers_quality.go](../../internal/api/handlers_quality.go)
