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

Reference implementation: `GetNativeRecommendations()` in
`internal/model/recommendation_set_native.go`.

Known paths still using the anti-pattern (fix when touching):

- `GetRecommendationQuality()` in `internal/model/recommendation_quality.go`
- Namespace list subqueries (audit baseline in `scripts/explain-audit/main.go`)

### 2. Index coverage

Verify indexes exist for filter + sort columns with matching partial predicates:

| Query pattern | Expected index shape |
|---------------|---------------------|
| Container list (keyset) | `(org_id, namespace, workload, container_name) WHERE stale = false` → `idx_rs_keyset_page` |
| Savings aggregation | `(org_id, cluster_uuid) INCLUDE (estimated_monthly_savings_usd) WHERE stale = false AND term = 'medium' AND engine = 'cost'` → `idx_rs_savings_agg` |
| History by org | `(org_id, recorded_at DESC)` → `idx_rh_org_recorded` |
| Namespace list | `(org_id, updated_at DESC) WHERE term IS NOT NULL AND stale = false` → `idx_ns_org_updated` |
| Digest lookback | `(org_id, cluster_uuid, schedule_type, bucket_date)` → `idx_daily_container_digests_lookback` |

Rules:

- Leading column = `org_id` on all org-scoped indexes
- Partial `WHERE` must match query filters exactly
- Keyset pagination index column order must match `ORDER BY`

See migrations `000078`, `000079`, `000076` and `migrations/README.md`.

### 3. DISTINCT and pagination

- Container list stores 6 rows per container — `SELECT DISTINCT` over large orgs is expensive
- Prefer **keyset pagination** (cursor params) over OFFSET for user-facing lists
- Use `org_recommendation_stats.container_count` via `GetOrgContainerCount()` — never `COUNT(DISTINCT ...)` on hot paths
- Deep OFFSET is O(offset) — flag any new OFFSET-based list endpoints

### 4. Savings and aggregation queries

- Avoid multiple correlated subqueries scanning the same org partition
- Use partial covering indexes with `INCLUDE` for sum columns
- Consider whether results belong in a materialized summary table (P2)

### 5. Run the EXPLAIN audit

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
| `Sort Method: external merge Disk:` | DISTINCT/sort too large — keyset, stats table, or materialized view |
| `Heap Fetches` high on index scan | Add INCLUDE columns (P1 index) |
| `actual rows` >> `rows` estimate | Run ANALYZE; check statistics |
| OFFSET in plan + >50 ms | Switch to keyset pagination |

## Fix Priority

| Priority | Fix type | Examples |
|----------|----------|----------|
| P0 | Query rewrite | org_id filter, keyset pagination, remove rh_accounts join |
| P1 | New index | partial index, covering INCLUDE, ORDER BY match |
| P2 | Architecture | materialized container keys, fleet savings summary |

## Verification Steps

Before approving query changes:

1. Confirm org scoping uses `org_id` directly (grep for `rh_accounts` in the query)
2. Confirm RBAC still works — `ApplyNativeRBAC` applied where needed
3. Check migration adds matching partial index if new filters introduced
4. Run explain-audit or manual `EXPLAIN (ANALYZE, BUFFERS)` at org-large scale
5. Target: list queries <100 ms; first-page keyset <5 ms on seeded 200k org
6. Run existing integration tests: `go test ./internal/model/... ./internal/api/...`

## Symptom → Fix Quick Reference

| Symptom | Fix |
|---------|-----|
| Seq scan on indexed column | Query rewrite (wrong join path) |
| Index scan but slow sort | Index matching ORDER BY |
| Heap fetches dominate | INCLUDE covering index |
| Filter on non-indexed predicate | Partial index matching WHERE |
| DISTINCT over millions of rows | Materialized view or key table (P2) |

## References

- Internal docs: [`docs/operations/query-performance.md`](../../../docs/operations/query-performance.md)
- Public docs: [`docs-site/query-performance.md`](../../../docs-site/query-performance.md)
- Audit script: [`scripts/explain-audit/`](../../../scripts/explain-audit/)
- Index conventions: [`migrations/README.md`](../../../migrations/README.md)
