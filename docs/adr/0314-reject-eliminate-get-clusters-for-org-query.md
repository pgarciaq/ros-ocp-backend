# ADR-0314: Reject eliminating getClustersForOrg query (PERF-07)

## Status

Rejected

## Context

The `getClustersForOrg` query runs on every list/detail request to fetch the set of clusters the caller has access to. The proposal was to eliminate it by inlining the cluster filter into the main query, arguing it was redundant with the org_id filter.

## Decision

Won't Fix. The query enforces intra-org RBAC cluster access control. Organizations may have multiple clusters with per-cluster role assignments. Removing this query would bypass cluster-level access control, allowing users to see recommendations for clusters they shouldn't have access to.

## Consequences

The extra query remains (~1ms overhead). Security boundary preserved.

## References

- [Performance audit v3](../performance/native-engine-audit-v3-2026-07.md) — PERF-07
- [#234](https://github.com/pgarciaq/ros-ocp-backend/issues/234)
