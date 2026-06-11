# ADR-0206: Explain-audit query harness for list performance

## Status

Accepted

## Context

List query performance is critical (dashboard loads). Index usage must be verified after query changes. Manual EXPLAIN runs are error-prone and not repeatable across environments.

## Decision

`scripts/explain-audit/main.go` runs `EXPLAIN (ANALYZE, BUFFERS)` on canonical list/detail/history queries with representative data. Asserts index usage patterns (e.g., `org_id` index on all detail lookups).

Complements [ADR-0143](0143-dry-run-sql-org-id-assertion.md) dry-run tests with actual execution plans.

## Consequences

- CI-adjacent tool (not in main test suite due to data requirements).
- Catches index regressions before production.
- Must be updated when new query patterns are added.

## Alternatives Considered

### pg_stat_statements in production

Reactive, not preventive. Rejected as sole strategy.

### Unit test EXPLAIN

Test DB may have different stats/plans than production-like data. Insufficient alone.

### Manual review

Doesn't scale with query surface area. Rejected.

## Related Decisions

- [ADR-0143](0143-dry-run-sql-org-id-assertion.md): Dry-run SQL org_id assertion tests.
- [ADR-0189](0189-precomputed-org-recommendation-stats.md): Pre-computed counts for list performance.

## References

- [scripts/explain-audit/main.go](../../scripts/explain-audit/main.go)
- [docs/operations/query-performance.md](../operations/query-performance.md)
