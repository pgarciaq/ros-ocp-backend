# 0306 — Add `is_stale` Column to `org_container_keys` for Unified Query Path

**Status:** Accepted
**Date:** 2026-06-30
**Domain:** Performance / Data Model
**Phase:** 15+
**Issue:** [#42](https://github.com/pgarciaq/ros-ocp-backend/issues/42)

## Context

The `org_container_keys` table was introduced in
[ADR 0052](0052-org-container-keys-denormalized-index.md) as a pre-computed
keyset pagination index. It stored only active (non-stale) container keys,
which meant `filter[stale]=true` and `filter[stale]=only` queries had to
bypass the fast keys path and fall back to `getNativeRecommendationsDistinct`,
a function that runs expensive `DISTINCT ON` queries over `recommendation_sets`.

Additionally, a correctness bug existed: `getNativeRecommendationsDistinct`
hardcoded `WHERE rs.stale = false`, which caused:

- `filter[stale]=true` (show both stale and non-stale) to return the same
  results as the default — effectively broken.
- `filter[stale]=only` to produce a contradictory `stale = false AND stale = true`
  predicate, returning zero rows.

## Decision

Add an `is_stale BOOLEAN NOT NULL DEFAULT false` column to `org_container_keys`
and modify `RefreshOrgContainerKeysTx` to upsert ALL containers (stale and
non-stale), setting `is_stale` from the latest `recommendation_sets.stale` value.

This allows `usesOrgContainerKeys()` to return `true` unconditionally, routing
all list queries (regardless of stale filter value) through the fast keyset
pagination path.

### Alternatives considered

**Option B — Maintain a parallel `org_container_stale_keys` table:** Doubles
storage and refresh complexity. Dismissed because the `is_stale` flag adds
only one byte per row and avoids synchronization issues.

**Option C — Remove the stale filter from `usesOrgContainerKeys` routing and
apply it only at the detail level:** Correct but slower — the keys page would
return containers that get filtered out during detail join, wasting I/O and
producing inconsistent pagination counts.

## Consequences

### Positive

- All three `filter[stale]` modes (`false`, `true`, `only`) use the same
  keyset pagination path, eliminating expensive `DISTINCT ON` fallback queries.
- The correctness bug is fixed: `filter[stale]=true` returns both stale and
  non-stale; `filter[stale]=only` returns only stale.
- A partial index on `(org_id) WHERE is_stale = true` keeps the B-tree compact
  since most containers are non-stale.

### Negative

- `RefreshOrgContainerKeysTx` now processes all containers instead of only
  active ones, marginally increasing refresh cost.
- The `org_container_keys` table grows to include stale containers (typically
  a small fraction of total).

### Neutral

- `getNativeRecommendationsDistinct` retains its hardcoded `stale = false`
  removal as a defense-in-depth fix, though it is now unreachable for normal
  list queries.
- `workload_type` filter atoms were removed from `nativeRecKeysFilterAtoms`
  because `org_container_keys` collapses `workload_type` (it is not part of
  the primary key). Workload-type filtering now applies only at the detail
  join level. This is an architectural correction exposed by routing all
  queries through the keys path.
