# ADR-0313: Reject factoring out rh_accounts subquery (PERF-02)

## Status

Rejected

## Context

The `rh_accounts` subquery appears in several list queries as `SELECT org_id FROM rh_accounts WHERE account = $1`. The proposal was to factor it into a separate lookup and pass the `org_id` directly to eliminate repeated subquery execution.

## Decision

Won't Fix. The subquery is not correlated — it references only the bind parameter `$1`, not outer table columns. PostgreSQL evaluates it once as an InitPlan (scalar substitution) and substitutes the result into the outer query. EXPLAIN confirms zero additional cost. Factoring it out would add a round-trip and complicate the code for no measurable gain.

## Consequences

Subquery remains inline. No performance impact; PostgreSQL optimizer handles it optimally.

## References

- [Performance audit v3](../performance/native-engine-audit-v3-2026-07.md) — PERF-02
- [#212](https://github.com/pgarciaq/ros-ocp-backend/issues/212)
