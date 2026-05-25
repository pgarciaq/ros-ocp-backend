# Query Performance — EXPLAIN ANALYZE Audit

Operational guide for PostgreSQL query performance in ROS-OCP Backend, based on a
2026 EXPLAIN ANALYZE audit of hot API paths at realistic scale (200k containers
≈ 1.2M `recommendation_sets` rows).

**Related:**

- Audit script: [`scripts/explain-audit/`](../../scripts/explain-audit/)
- Migrations: [`000078_keyset_pagination_indexes.up.sql`](../../migrations/000078_keyset_pagination_indexes.up.sql), [`000079_explain_audit_indexes.up.sql`](../../migrations/000079_explain_audit_indexes.up.sql), [`000080_explain_audit_plugin_indexes.up.sql`](../../migrations/000080_explain_audit_plugin_indexes.up.sql)
- Index conventions: [`migrations/README.md`](../../migrations/README.md)
- Container list implementation: [`internal/model/recommendation_set_native.go`](../../internal/model/recommendation_set_native.go)
- Savings summary: [`internal/api/handlers_savings_summary.go`](../../internal/api/handlers_savings_summary.go)

**Last updated:** 2026-05-25

---

## Background

Most recommendation tables carry a denormalized `org_id` column (added during the
native-engine migration). The `clusters` and `rh_accounts` tables still exist for
tenant registration and RBAC, but **org scoping must not depend on joining through
them** when `org_id` is available on the queried table.

The audit seeded three org sizes (`org-small` 1k, `org-medium` 50k, `org-large`
200k containers) and ran `EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)` on every hot
query path via [`scripts/explain-audit/main.go`](../../scripts/explain-audit/main.go).

---

## The `rh_accounts` Join Anti-Pattern

### Problem

When a table has `org_id` but the query joins `clusters → rh_accounts` to resolve
the org, PostgreSQL cannot use org-scoped indexes on the fact table. At 200k
containers this pattern was **~5,000× slower** than filtering `org_id` directly.

Symptoms in `EXPLAIN ANALYZE`:

- `Nested Loop` through `clusters` → `rh_accounts` for org-level filtering
- `Seq Scan` on tables that have `org_id` indexed
- `Sort` with `external merge` (disk spill) for `DISTINCT` / `ORDER BY`

### Bad pattern (pre-fix audit baseline)

```sql
SELECT DISTINCT rs.cluster_uuid, rs.namespace, rs.workload, rs.container_name
FROM recommendation_sets rs
JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid
JOIN rh_accounts ra ON ra.id = c.tenant_id
WHERE ra.org_id = $1 AND rs.stale = false;
```

Example plan fragment (org-large, ~1.2M rows):

```
Nested Loop  (actual time=842.3..1247.8 rows=200000 loops=1)
  ->  Seq Scan on recommendation_sets rs  (actual rows=1200000)
        Filter: (NOT stale)
  ->  Index Scan using clusters_pkey on clusters c
  ->  Index Scan using rh_accounts_pkey on rh_accounts ra
        Filter: (org_id = 'org-large'::text)
Sort  (actual time=1251.2..1289.4 rows=200000 loops=1)
  Sort Method: external merge  Disk: 98432kB
Execution Time: 1294.7 ms
```

### Good pattern (current container list)

```sql
SELECT DISTINCT rs.cluster_uuid, rs.namespace, rs.workload, rs.container_name
FROM recommendation_sets rs
JOIN clusters c ON c.cluster_uuid = rs.cluster_uuid   -- RBAC / metadata only
WHERE rs.org_id = $1 AND rs.stale = false;
```

Example plan fragment with keyset pagination (first page):

```
Index Scan using idx_rs_keyset_page on recommendation_sets rs
  Index Cond: (org_id = 'org-large'::text)
  Filter: (NOT stale)
  Rows Removed by Filter: 0
Execution Time: 0.4 ms
```

Implementation: [`GetNativeRecommendations()`](../../internal/model/recommendation_set_native.go) — the distinct subquery filters on `rs.org_id` directly; the `clusters` join is used only for RBAC (`ApplyNativeRBAC`) and returning `source_id` / `cluster_alias`.

### Rule

| Use case | Join `clusters` / `rh_accounts`? |
|----------|----------------------------------|
| Org scoping (`WHERE org_id = ?`) | **No** — filter on denormalized `org_id` |
| RBAC cluster access (`cluster_uuid IN (...)`) | **Yes** — join `clusters` only |
| Return cluster metadata (alias, source_id) | **Yes** — join `clusters` only |
| Resolve org when table has no `org_id` | **Yes** — last resort |

### Remaining offenders to fix

These paths still join through `rh_accounts` for org scoping and should be migrated
to direct `org_id` filters where the column exists:

| File | Function / query |
|------|------------------|
| [`internal/model/recommendation_quality.go`](../../internal/model/recommendation_quality.go) | `GetRecommendationQuality` |
| [`internal/model/recommendation_set_native.go`](../../internal/model/recommendation_set_native.go) (namespace list SQL in audit) | Namespace list subqueries |
| History list queries in audit | `recommendation_history` (index added; query rewrite pending) |
| [`internal/engine/recommend_business_hours.go`](../../internal/engine/recommend_business_hours.go) | Multi-cluster BH digest enrichment when page spans all clusters |

**Fixed in this audit:**

| File | Change |
|------|--------|
| [`handlers_node_utilization.go`](../../internal/api/handlers_node_utilization.go) | Org filter via `node_recommendations.org_id` |
| [`node_gpu_triples.go`](../../internal/engine/node_gpu_triples.go) | Drop `rh_accounts` join; trust RBAC-scoped cluster list |

---

## Index Design Principles

### 1. Partial indexes match query predicates

Most list queries filter `WHERE stale = false` and savings queries add
`term = 'medium' AND engine = 'cost'`. Partial indexes that include these
predicates are dramatically smaller and faster than full-table indexes.

```sql
-- Keyset pagination (migration 000078)
CREATE INDEX idx_rs_keyset_page
    ON recommendation_sets (org_id, namespace, workload, container_name)
    WHERE stale = false;

-- Savings aggregation (migration 000079)
CREATE INDEX idx_rs_savings_agg
    ON recommendation_sets (org_id, cluster_uuid)
    INCLUDE (estimated_monthly_savings_usd)
    WHERE stale = false AND term = 'medium' AND engine = 'cost';
```

### 2. `INCLUDE` columns for covering indexes

Savings aggregation sums `estimated_monthly_savings_usd` grouped by org/cluster.
An index-only scan avoids heap fetches when the aggregate column is in `INCLUDE`.

### 3. Leading column = `org_id`

Virtually every API query starts with org scoping. All list/aggregation indexes
should lead with `org_id`.

### 4. Keyset pagination index must match `ORDER BY`

The container list natural sort is `(namespace, workload, container_name)`.
The keyset index `(org_id, namespace, workload, container_name) WHERE stale = false`
must match exactly — mismatched column order prevents index scans for pagination.

### 5. Production: use `CREATE INDEX CONCURRENTLY`

See [`migrations/README.md`](../../migrations/README.md) for pre-migration
concurrent index steps on large deployments.

---

## DISTINCT Is Expensive at Scale

### Problem

`recommendation_sets` stores **6 rows per container** (3 terms × 2 engines).
The list API returns distinct containers. At 200k containers = 1.2M rows,
`SELECT DISTINCT ...` must sort the entire org partition even with a supporting
index.

### Measured impact (org-large)

| Approach | Page | Execution time |
|----------|------|----------------|
| Keyset pagination | Page 1 | ~0.4 ms |
| Keyset pagination | Page 2+ | ~1 ms (still requires DISTINCT sort over scanned range) |
| OFFSET pagination | Page 1 | ~50 ms |
| OFFSET pagination | Page 500 | ~1,200 ms |
| `COUNT(DISTINCT ...)` subquery | Full org | ~800 ms |

### Mitigations (implemented)

1. **Keyset pagination** — `idx_rs_keyset_page` + cursor params
   (`after_namespace`, `after_workload`, `after_container`). First page is sub-ms.
2. **Pre-computed counts** — `org_recommendation_stats.container_count` via
   [`GetOrgContainerCount()`](../../internal/model/org_recommendation_stats.go).
   Updated on ingestion; avoids `COUNT(DISTINCT ...)` on every list request.
3. **Deprecate deep OFFSET** — offset page 500 is O(offset) by design.

### Future work

- Materialized **container key table** (one row per container, terms joined at read time)
- Or periodic refresh of a materialized view for distinct container keys

---

## Savings Aggregation

### Problem

The fleet savings summary runs **4 correlated subqueries** (containers, nodes,
PVCs, namespaces), each scanning the full org partition. Without covering indexes,
each subquery performs heap fetches for `estimated_monthly_savings_usd`.

### Fix

Partial covering index `idx_rs_savings_agg` (see above). PVC and node tables have
similar org-scoped partial indexes from migration 000061.

### Future work

- Materialized per-org fleet summary table refreshed on ingestion
- Single `UNION ALL` aggregation instead of 4 correlated subqueries

Source: [`handlers_savings_summary.go`](../../internal/api/handlers_savings_summary.go)

---

## Plugin-Specific Audit Results (2026-05-25)

Expanded audit coverage for GPU, snapshot, business hours, term filters, and node
utilization at `org-large` scale (200k containers, ~95k GPU digest rows, 12k
snapshot recommendations). Times below are from `EXPLAIN ANALYZE` after seed +
`ANALYZE` on a warm cache.

### GPU MIG (`QueryGPURecommendations`)

**Path:** [`internal/engine/gpu_query.go`](../../internal/engine/gpu_query.go) →
[`GetGPUMIGRecommendations`](../../internal/api/handlers_gpu_mig.go)

| Query | Time | Scan | Verdict |
|-------|------|------|---------|
| Per-cluster digest lookback (30 days) | ~21 ms | Seq scan on single GPU partition (~32k rows) | **Healthy** — cluster-scoped; MIG scoring runs in Go after fetch |

The handler loads digests per cluster (RBAC-resolved UUIDs), then computes MIG
profiles in memory. No `rh_accounts` join on the digest table.

### GPU time-slicing (`CountNodeGPUTriples` / `ListNodeGPUTriplesPage`)

**Path:** [`internal/engine/node_gpu_triples.go`](../../internal/engine/node_gpu_triples.go) →
[`GetNodeRecommendations`](../../internal/api/handlers_node_recs.go)

| Query | Time | Scan | Verdict |
|-------|------|------|---------|
| Count distinct (cluster, node, model) triples | ~115–157 ms | Seq scan on GPU partitions + fresh-node subquery | **Borderline** — acceptable at current seed scale; monitor at millions of GPU digest rows |
| List triples page (LIMIT 10) | ~116–168 ms | Same | **Borderline** |

**Fix applied:** Removed redundant `clusters → rh_accounts` joins from triple
pagination SQL. Org scoping is enforced upstream via `getClustersForOrg()` +
RBAC; the query filters `cluster_uuid = ANY($clusters)`.

**Index added (000080):** `idx_gpu_digest_cluster_interval_node` on
`(cluster_uuid, interval_start DESC, node_name)` — supports the freshness
`HAVING MAX(interval_start)` subquery.

**Future work:** The fresh-node subquery scans GPU digests twice (once for
freshness, once for grouping). A materialized “latest node seen” table refreshed
on ingestion would cut this to a single probe.

### Snapshot staleness

**Paths:** [`handlers_snapshot.go`](../../internal/api/handlers_snapshot.go) (list),
[`snapshot_classify.go`](../../internal/engine/snapshot_classify.go) (classify + reconcile)

| Query | Time | Scan | Verdict |
|-------|------|------|---------|
| List org (`ORDER BY age_days DESC`) | ~0.1 ms | Index scan `idx_snapshot_recs_org_age` | **Healthy** |
| Count org | ~1 ms | Seq scan (small table at 12k rows) | **Healthy** |
| Classify inventory (`DISTINCT ON` fresh window) | ~0 ms | Index scan on `ingested_at` | **Healthy** |
| Reconcile freshness gate (two `COUNT` subqueries) | ~0.3 ms | Index scan | **Healthy** |

All snapshot paths already filter on denormalized `org_id`. Migration 000080 adds
`idx_snapshot_recs_org_age` and `idx_snapshot_inventory_org_cluster_ingested`.

### Business hours dual-stream

**Paths:** [`recommend_business_hours.go`](../../internal/engine/recommend_business_hours.go)
(digest enrichment), container list (`dual_list_all_terms` on `recommendation_sets`)

| Query | Time | Scan | Verdict |
|-------|------|------|---------|
| Single-cluster BH digest lookback | ~220–297 ms | Bitmap/index on lookback index | **Acceptable** — matches one cluster on a list page |
| All-cluster BH digest (5 clusters) | ~5.6 s | Parallel seq scan on 3M digest rows | **Needs attention** — worst-case when list page spans every cluster |
| Schedule lookup | ~0 ms | Bitmap on `business_hours_schedules` | **Healthy** |
| Dual term rows per container (`all_hours` in `recommendation_sets`) | ~0 ms | Index scan `idx_rs_keyset_page` | **Healthy** |

The enrichment query [`QueryContainerDigestsByScheduleTypeForClusters`](../../internal/engine/recommend_business_hours.go)
loads **all** BH digest rows for every cluster present on the current list page.
Typical pages hit 1–2 clusters (~220 ms). A page spanning all clusters in a
large org triggers a multi-million-row scan.

**Future work:** Restrict BH digest fetch to container keys on the page (namespace/
workload/container tuple filter), or run per-cluster queries in parallel with
smaller limits.

### Term / engine filter performance

Container list applies `term` and `engine` as post-`DISTINCT` filters on
`recommendation_sets`. The keyset index
`(org_id, namespace, workload, container_name) WHERE stale = false` does **not**
include `term` or `engine` because pagination keys are container-level, not
term-level.

| Filter | Time | Verdict |
|--------|------|---------|
| `term = short/medium/long` | ~1.2–1.4 s | **Same cost as unfiltered list** — dominated by `DISTINCT` sort |
| `engine = cost/performance` | ~1.0–1.5 s | **Same** |
| `term = short AND engine = performance` | ~1.2 s | **Same** |

Adding partial indexes per term/engine would shrink index size but would **not**
help the list API unless the query pattern changes to filter before `DISTINCT`.
No migration needed today; document as architectural constraint.

### Node utilization (CPU/memory metrics)

**Path:** [`handlers_node_utilization.go`](../../internal/api/handlers_node_utilization.go)
— distinct from GPU time-slicing and node **sizing** recommendations.

| Query | Time | Scan | Verdict |
|-------|------|------|---------|
| Count distinct nodes | ~0.7 ms | Index-friendly after rewrite | **Healthy** |
| List page (LIMIT 10, nested terms/engines) | ~1.9 ms | Seq scan on `node_recommendations` (4020 rows/org) | **Healthy** |

**Fix applied:** Replaced `node_recommendations → clusters → rh_accounts` org
filter with direct `WHERE nr.org_id = $1`. Migration 000080 adds
`idx_nr_org_cluster_node (org_id, cluster_uuid, node)`.

---


## When to Add Indexes vs Rewrite Queries

| Symptom in EXPLAIN | Fix | Priority |
|--------------------|-----|----------|
| Seq scan on column that has an index | Query rewrite — wrong join path prevents index use | **P0** (free) |
| Index scan but expensive sort | Add index matching `ORDER BY` / keyset columns | **P1** (cheap migration) |
| Heap fetches dominate (`Buffers: shared hit` low vs reads) | Add `INCLUDE` columns for covering index | **P1** |
| Filter on non-indexed predicate (`stale`, `term`, `engine`) | Add partial index matching `WHERE` clause | **P1** |
| `DISTINCT` over millions of rows | Architectural change (materialized view or key table) | **P2** |
| Deep OFFSET pagination | Switch to keyset/cursor pagination | **P0** |

---

## Audit Methodology

### 1. Seed realistic data volumes

Target **200k+ containers** for the org under test. The audit seed creates:

| Org | Containers | `recommendation_sets` rows |
|-----|------------|----------------------------|
| `org-small` | 1,000 | ~6,000 |
| `org-medium` | 50,000 | ~300,000 |
| `org-large` | 200,000 | ~1,200,000 |

Seed script: [`scripts/explain-audit/seed.sql`](../../scripts/explain-audit/seed.sql)

### 2. Run EXPLAIN on every hot path

```sql
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) <query>;
```

The audit script covers:

- Container list (offset vs keyset, filters)
- Container count (stats lookup vs DISTINCT subquery)
- Digest lookback (all_hours, business_hours)
- Node, namespace, PVC lists
- Savings summary (by plugin, by cluster)
- History, quality, thresholds
- MarkAdopted batch update
- GPU MIG digest lookback (`QueryGPURecommendations`)
- GPU time-slicing triple pagination (`CountNodeGPUTriples`, `ListNodeGPUTriplesPage`)
- Snapshot list, classify, reconcile freshness gate
- Business hours digest enrichment (single- and multi-cluster)
- Term/engine filter variants on container list
- Node utilization list (distinct from node sizing recommendations)

### 3. Red flags

| Signal | Meaning |
|--------|---------|
| `Seq Scan` on large table | Missing index or index bypassed by join |
| `Sort Method: external merge Disk:` | Sort spilled to disk — too many rows |
| `Nested Loop` through FK chain | Likely org scoping via `rh_accounts` |
| `actual rows` >> `rows` (estimate) | Stale statistics — run `ANALYZE` |
| `Heap Fetches` high on index scan | Need covering index with `INCLUDE` |

### 4. Fix priority

1. **P0 — Query rewrites** (no migration, immediate deploy)
2. **P1 — New indexes** (migration + optional `CONCURRENTLY` pre-step)
3. **P2 — Architecture** (materialized views, denormalization, key tables)

---

## Reproducing the Audit

### Prerequisites

- PostgreSQL 16 (local container or existing dev DB)
- Go toolchain (same module as ros-ocp-backend)

### Steps

```bash
# 1. Start local PostgreSQL
podman run -d --name ros-explain-db -p 25432:5432 \
  -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=ros_explain postgres:16

# 2. Apply migrations
cd ~/dev/koku/ros-ocp-backend
DB_HOST=localhost DB_PORT=25432 DB_NAME=ros_explain \
DB_USER=postgres DB_PASSWORD=postgres DB_SSL=disable \
go run . db migrate up

# 3. Run seed + EXPLAIN audit
PGPASSWORD=postgres go run ./scripts/explain-audit/ \
  -db-host localhost -db-port 25432 -db-name ros_explain
```

### Useful flags

| Flag | Purpose |
|------|---------|
| `-seed-only` | Load data without running EXPLAIN |
| `-skip-seed` | Re-run EXPLAIN on existing data |

### Interpreting output

The script prints a markdown table per category with execution time, scan type,
and detected issues (slow >100ms, seq scans on large tables, OFFSET in plan).
See the **OFFSET vs KEYSET** comparison and **RECOMMENDATIONS** sections at the
end of the report.

---

## Checklist for New Queries

Before merging any new list or aggregation query:

- [ ] Org scoping uses denormalized `org_id`, not `rh_accounts` join
- [ ] `clusters` join present only for RBAC or metadata
- [ ] Index exists with `org_id` as leading column
- [ ] Partial index predicates match `WHERE stale = false` / term / engine filters
- [ ] `ORDER BY` columns match a supporting index (for pagination)
- [ ] No `COUNT(DISTINCT ...)` on hot path — use pre-computed stats or materialized data
- [ ] Keyset pagination preferred over OFFSET for user-facing lists
- [ ] Ran explain-audit script or manual `EXPLAIN ANALYZE` at org-large scale

---

## See Also

- [Configuration Reference](configuration.md) — connection pool tuning
- [Monitoring](monitoring.md) — API latency metrics
- [Native Engine Performance](../native-engine-performance.md) — ingestion-side optimizations
