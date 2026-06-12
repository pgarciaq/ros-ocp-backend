---
name: query-performance-review
description: >-
  Reviews PostgreSQL query performance for ros-ocp-backend list and aggregation
  endpoints. Use when adding or modifying API queries, reporting slow responses
  or timeouts, adding queryable table columns, or before merging database access
  code in internal/model or internal/api handlers.
---

# Query Performance Review

Review database queries in ros-ocp-backend against EXPLAIN ANALYZE audit findings.
Full methodology: [`docs/operations/query-performance.md`](../../../docs/operations/query-performance.md).

## When to Apply

Trigger this skill when:

- Someone adds a new query or list endpoint
- Someone modifies an existing query path in `internal/model/` or `internal/api/`
- Someone reports slow API responses or timeouts on list/aggregation endpoints
- Someone adds a new table or column that will be queried in list contexts
- A migration adds indexes — verify they match actual query predicates

## Review Checklist

### 1. Org scoping — `org_id` vs `rh_accounts` join

**Critical rule:** Filter on denormalized `org_id` directly. Never join
`clusters → rh_accounts` for org scoping when the table has `org_id`.

```sql
-- BAD: bypasses org_id indexes
JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid
JOIN rh_accounts ra ON ra.id = c.tenant_id
WHERE ra.org_id = $1

-- GOOD: uses partial index on org_id
WHERE rs.org_id = $1 AND rs.stale = false
```

Allow `clusters` join only for:

- RBAC: `c.cluster_uuid IN (...)` via `ApplyNativeRBAC`
- Metadata: `c.cluster_alias`, `c.source_id`

Reference implementation: `GetNativeRecommendations()` →
`getNativeRecommendationsFromOrgKeys()` in
`internal/model/recommendation_set_native.go`.

Known paths still using the anti-pattern (fix when touching):

- `GetRecommendationQuality()` in `internal/model/recommendation_quality.go`
- Namespace list subqueries (audit baseline in `scripts/explain-audit/main.go`)

### 2. Index coverage

Verify indexes exist for filter + sort columns with matching partial predicates:

| Query pattern | Expected index shape |
|---------------|---------------------|
| Container list pagination | `(org_id, namespace, workload, container_name)` on `org_container_keys` → `idx_ock_org_sorted` |
| Container list (legacy DISTINCT path) | `(org_id, namespace, workload, container_name) WHERE stale = false` on `recommendation_sets` → `idx_rs_keyset_page` |
| Tag filter (future) | GIN on `org_container_keys.resolved_tags` → `idx_ock_tags` |
| Cluster filter on key table | `(org_id, cluster_uuid)` → `idx_ock_org_cluster` |
| Savings aggregation | `(org_id, cluster_uuid) INCLUDE (estimated_savings_cents) WHERE stale = false AND term = 'medium' AND engine = 'cost'` → `idx_rs_savings_agg` |
| History by org | `(org_id, recorded_at DESC)` → `idx_rh_org_recorded` |
| Namespace list | `(org_id, updated_at DESC) WHERE term IS NOT NULL AND stale = false` → `idx_ns_org_updated` |
| Digest lookback | `(org_id, cluster_uuid, schedule_type, bucket_date)` → `idx_daily_container_digests_lookback` |

Rules:

- Leading column = `org_id` on all org-scoped indexes
- Partial `WHERE` must match query filters exactly
- Keyset pagination index column order must match `ORDER BY`

See migrations `000078`, `000079`, `000081` and `migrations/README.md`.

### 3. Pagination and container keys

**Critical rules:**

- **List queries should paginate `org_container_keys`, not `recommendation_sets` directly**
  — one row per container; join `recommendation_sets` only for the current page.
- **Never use `SELECT DISTINCT` on `recommendation_sets` for container list hot paths**
  — use the 2-step key-table pattern in `getNativeRecommendationsFromOrgKeys()`.
- Prefer **keyset pagination** (cursor params) over OFFSET for user-facing lists.
- Use `org_recommendation_stats.container_count` or `COUNT(*)` on `org_container_keys`
  — never `COUNT(DISTINCT ...)` on hot paths.
- Deep OFFSET on the key table is O(offset) but cheap; keyset is still preferred.

Tables to check when adding list features:

- `org_container_keys` — pagination keys, future tag filters (`resolved_tags`)
- `recommendation_sets` — term/engine detail for page containers only
- `org_recommendation_stats` — pre-computed container counts

Refresh: `RefreshOrgMetadata` runs once at end of streaming reconcile cycles
(ingest, threshold recalc); `MarkAdopted` still calls `RefreshOrgContainerKeys` immediately.

### 4. Enrichment queries (BH, GPU, etc.)

**Critical rule:** **Enrichment functions must filter by page container keys, never load entire clusters.**

- BH: `QueryContainerDigestsByScheduleTypeForContainers` with `PageContainerDigestKey` tuples
- Do not call cluster-wide digest loaders (`ForClusters`) from list enrichment paths
- Scope digest/GPU lookups to containers (or namespaces) on the current API page

Reference: `EnrichNativeContainerResultsWithBusinessHours()` in
`internal/engine/recommend_business_hours.go`.

### 5. Savings and aggregation queries

- Avoid multiple correlated subqueries scanning the same org partition
- Use partial covering indexes with `INCLUDE` for sum columns
- Consider whether results belong in a materialized summary table (P2 — fleet savings still open)

### 6. Run the EXPLAIN audit

For non-trivial query changes, run the audit script against a seeded local DB:

```bash
podman run -d --name ros-explain-db -p 25432:5432 \
  -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=ros_explain postgres:16

cd ~/dev/koku/ros-ocp-backend
DB_HOST=localhost DB_PORT=25432 DB_NAME=ros_explain \
DB_USER=postgres DB_PASSWORD=postgres DB_SSL=disable \
go run . db migrate up

PGPASSWORD=postgres go run ./scripts/explain-audit/ \
  -db-host localhost -db-port 25432 -db-name ros_explain
```

Add a new query case to `scripts/explain-audit/main.go` if introducing a new hot path.

## EXPLAIN Red Flags

| Plan node | Action |
|-----------|--------|
| `Seq Scan` on large table with org_id index | Fix join path (P0 rewrite) |
| `Nested Loop` through clusters → rh_accounts | Replace with direct org_id filter |
| `Sort Method: external merge Disk:` | Use `org_container_keys` pagination, not DISTINCT |
| `Heap Fetches` high on index scan | Add INCLUDE columns (P1 index) |
| `actual rows` >> `rows` estimate | Run ANALYZE; check statistics |
| OFFSET in plan + >50 ms on recommendation_sets | Paginate `org_container_keys` instead |

## Fix Priority

| Priority | Fix type | Examples |
|----------|----------|----------|
| P0 | Query rewrite | org_id filter, keyset pagination, remove rh_accounts join, page-scoped enrichment |
| P1 | New index | partial index, covering INCLUDE, ORDER BY match, GIN on resolved_tags when tags ship |
| P2 | Architecture | ~~materialized container keys~~ **done**; fleet savings summary; GPU fresh-node table |

## Verification Steps

Before approving query changes:

1. Confirm org scoping uses `org_id` directly (grep for `rh_accounts` in the query)
2. Confirm RBAC still works — `ApplyNativeRBAC` applied where needed
3. Confirm container lists paginate `org_container_keys`, not DISTINCT on `recommendation_sets`
4. Confirm enrichment queries scope to page container keys
5. Check migration adds matching partial index (and GIN on `resolved_tags` if adding tag filters)
6. Run explain-audit or manual `EXPLAIN (ANALYZE, BUFFERS)` at org-large scale
7. Target: list queries <100 ms; key-table pagination <5 ms on seeded 200k org
8. Run existing integration tests: `go test ./internal/model/... ./internal/api/...`

## Symptom → Fix Quick Reference

| Symptom | Fix |
|---------|-----|
| Seq scan on indexed column | Query rewrite (wrong join path) |
| Index scan but slow sort | Paginate `org_container_keys`; index matching ORDER BY |
| Heap fetches dominate | INCLUDE covering index |
| Filter on non-indexed predicate | Partial index matching WHERE |
| DISTINCT over millions of rows | `org_container_keys` key table (implemented) |
| BH/GPU enrichment slow on multi-cluster page | Filter digests by page container keys |

## References

- Internal docs: [`docs/operations/query-performance.md`](../../../docs/operations/query-performance.md)
- Public docs: [`docs-site/query-performance.md`](../../../docs-site/query-performance.md)
- Audit script: [`scripts/explain-audit/`](../../../scripts/explain-audit/)
- Index conventions: [`migrations/README.md`](../../../migrations/README.md)
- Key table: [`internal/model/org_container_keys.go`](../../../internal/model/org_container_keys.go)
