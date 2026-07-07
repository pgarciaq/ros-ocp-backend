# ADR-0315: Reject pushing full RBAC filter into SQL (PERF-08 / DB-007)

## Status

Rejected

## Context

RBAC filtering is applied in two stages: cluster-level filtering is pushed into SQL (`WHERE cluster_uuid = ANY($4)`), while node-level filtering is applied in Go after fetching results. The proposal was to push node-level filtering into SQL as well.

## Decision

Won't Fix. The cluster-level filter (the high-cardinality dimension) is already in SQL, which eliminates >99% of irrelevant rows at the database level. The remaining node-level Go-side discard is low ROI because:

1. Node-level RBAC is a rare configuration (most orgs use cluster-level only).
2. When present, node lists are small (~10–100 items after cluster filtering).
3. Adding node filtering to SQL would complicate dynamic query construction for marginal gain.

## Consequences

Node-level RBAC filtering remains in Go. Performance impact negligible for typical deployments.

## References

- [Performance audit v3](../performance/native-engine-audit-v3-2026-07.md) — DB-007 / PERF-08
- [#202](https://github.com/pgarciaq/ros-ocp-backend/issues/202)
