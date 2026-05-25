# Query Performance — EXPLAIN ANALYZE Audit

Operational guide for PostgreSQL query performance in ROS-OCP Backend, based on a
2026 EXPLAIN ANALYZE audit of hot API paths at realistic scale (200k containers
≈ 1.2M `recommendation_sets` rows).

**Related:**

- Audit script: [`scripts/explain-audit/`](../../scripts/explain-audit/)
- Migrations: [`000078_keyset_pagination_indexes.up.sql`](../../migrations/000078_keyset_pagination_indexes.up.sql), [`000079_explain_audit_indexes.up.sql`](../../migrations/000079_explain_audit_indexes.up.sql), [`000080_explain_audit_plugin_indexes.up.sql`](../../migrations/000080_explain_audit_plugin_indexes.up.sql), [`000081_create_org_container_keys.up.sql`](../../migrations/000081_create_org_container_keys.up.sql)
- Index conventions: [`migrations/README.md`](../../migrations/README.md)
- Container list implementation: [`internal/model/recommendation_set_native.go`](../../internal/model/recommendation_set_native.go)
- Container key table: [`internal/model/org_container_keys.go`](../../internal/model/org_container_keys.go)
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

Two-step pagination via `org_container_keys` (no `DISTINCT` on `recommendation_sets`):

```sql
-- Step 1: page container keys (one row per container)
SELECT ock.cluster_uuid, ock.namespace, ock.workload, ock.container_name
FROM org_container_keys ock
JOIN clusters c ON c.cluster_uuid = ock.cluster_uuid   -- RBAC / metadata only
WHERE ock.org_id = $1
ORDER BY ock.namespace, ock.workload, ock.container_name
LIMIT 11;

-- Step 2: join term/engine rows for containers on the page
SELECT rs.*
FROM recommendation_sets rs
JOIN (/* page keys subquery */) page ON ...
WHERE rs.org_id = $1 AND rs.stale = false;
```

Example plan fragment with keyset pagination (first page):

```
Index Scan using idx_ock_org_sorted on org_container_keys ock
  Index Cond: (org_id = 'org-large'::text)
Execution Time: 0.3 ms
```

Implementation: [`GetNativeRecommendations()`](../../internal/model/recommendation_set_native.go) →
[`getNativeRecommendationsFromOrgKeys()`](../../internal/model/recommendation_set_native.go) —
paginates `org_container_keys` with `idx_ock_org_sorted`, then joins `recommendation_sets` for
term/engine detail. The `clusters` join is used only for RBAC (`ApplyNativeRBAC`) and returning
`source_id` / `cluster_alias`.

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
**Fixed in this audit:**

| File | Change |
|------|--------|
| [`handlers_node_utilization.go`](../../internal/api/handlers_node_utilization.go) | Org filter via `node_recommendations.org_id` |
| [`node_gpu_triples.go`](../../internal/engine/node_gpu_triples.go) | Drop `rh_accounts` join; trust RBAC-scoped cluster list |
| [`recommend_business_hours.go`](../../internal/engine/recommend_business_hours.go) | BH enrichment uses `QueryContainerDigestsByScheduleTypeForContainers` — page keys only |
| [`recommendation_set_native.go`](../../internal/model/recommendation_set_native.go) | Container list paginates `org_container_keys`; term/engine filters on detail join |

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

## DISTINCT Is Expensive at Scale — Fixed via `org_container_keys`

### Problem (pre-fix)

`recommendation_sets` stores **6 rows per container** (3 terms × 2 engines).
The list API returns distinct containers. At 200k containers = 1.2M rows,
`SELECT DISTINCT ...` must sort the entire org partition even with a supporting
index. Term/engine filters made this worse (~1.2 s) because the sort ran before
those predicates could narrow the set.

### Measured impact (org-large, pre-fix baseline)

| Approach | Page | Execution time |
|----------|------|----------------|
| DISTINCT + keyset pagination | Page 1 | ~0.4 ms |
| DISTINCT + keyset pagination | Page 2+ | ~1 ms (DISTINCT sort over scanned range) |
| DISTINCT + term/engine filter | Any page | ~1.2 s |
| OFFSET pagination | Page 500 | ~1,200 ms |
| `COUNT(DISTINCT ...)` subquery | Full org | ~800 ms |

### Fix: `org_container_keys` materialized key table

Migration [`000081_create_org_container_keys.up.sql`](../../migrations/000081_create_org_container_keys.up.sql)
adds one row per active container. List queries use a **2-step pattern**:

1. **Page keys** — `SELECT ... FROM org_container_keys` with `idx_ock_org_sorted`
   (keyset or offset). No `DISTINCT`; cost is O(page size) at any depth.
2. **Fetch detail** — `JOIN recommendation_sets` for containers on the page only;
   apply `term` / `engine` filters here (6 rows × page limit).

Refresh: [`RefreshOrgContainerKeys()`](../../internal/model/org_container_keys.go) runs after
[`WriteRecommendations`](../../internal/engine/recommend_all.go) and after
[`MarkAdopted`](../../internal/engine/adoption.go).

### Measured impact (org-large, post-fix)

| Approach | Page | Execution time |
|----------|------|----------------|
| Key table + keyset pagination | Page 1 | ~0.3 ms |
| Key table + keyset pagination | Page 500+ | **< 5 ms** (no DISTINCT) |
| Key table + term/engine filter | Any page | **< 5 ms** |
| Container count | Full org | ~0 ms (pre-computed stats or `COUNT(*)` on key table) |

### Other mitigations (still in place)

1. **Keyset pagination** — cursor params (`after_namespace`, `after_workload`,
   `after_container`) on the key table sort order.
2. **Pre-computed counts** — `org_recommendation_stats.container_count` via
   [`GetOrgContainerCount()`](../../internal/model/org_recommendation_stats.go),
   with fallback to `COUNT(*)` on `org_container_keys`.
3. **Legacy DISTINCT path** — [`getNativeRecommendationsDistinct()`](../../internal/model/recommendation_set_native.go)
   retained only when the key table path does not apply (non-default stale filter).

See [org_container_keys](#org_container_keys-table) below for schema and future tag filtering.

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
| All-cluster BH digest (5 clusters, pre-fix) | ~5.6 s | Parallel seq scan on 3M digest rows | **Was worst case** — page spanned every cluster |
| All-cluster BH digest (post-fix) | **< 5 ms** | Index probe on page container tuples | **Fixed** — container-key filter |
| Schedule lookup | ~0 ms | Bitmap on `business_hours_schedules` | **Healthy** |
| Dual term rows per container (`all_hours` in `recommendation_sets`) | ~0 ms | Index scan on key table + detail join | **Healthy** |

**Fix applied:** [`EnrichNativeContainerResultsWithBusinessHours`](../../internal/engine/recommend_business_hours.go)
now calls [`QueryContainerDigestsByScheduleTypeForContainers`](../../internal/engine/recommend_business_hours.go),
which restricts the digest query to `(cluster_uuid, namespace, workload, container_name)`
tuples on the current list page via `unnest` + `IN` — never loads an entire cluster
partition. Typical pages (~10 containers) stay sub-ms regardless of how many clusters
appear on the page.

### Term / engine filter performance

Container list paginates via `org_container_keys`, then applies `term` and `engine`
filters on the detail join to `recommendation_sets` (page size × 6 rows max).

| Filter | Time (pre-fix) | Time (post-fix) | Verdict |
|--------|----------------|-----------------|---------|
| `term = short/medium/long` | ~1.2–1.4 s | **< 5 ms** | **Fixed** — no DISTINCT |
| `engine = cost/performance` | ~1.0–1.5 s | **< 5 ms** | **Fixed** |
| `term = short AND engine = performance` | ~1.2 s | **< 5 ms** | **Fixed** |

Partial indexes per term/engine on `recommendation_sets` are optional for the detail
join; pagination no longer depends on them.

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

## `org_container_keys` Table

Migration [`000081_create_org_container_keys.up.sql`](../../migrations/000081_create_org_container_keys.up.sql).
Implementation: [`internal/model/org_container_keys.go`](../../internal/model/org_container_keys.go).

### Purpose

Pre-materialized unique container keys for pagination. One row per active container
eliminates runtime `DISTINCT` over 1.2M `recommendation_sets` rows and provides a
stable surface for future tag-based list filters.

### Schema

| Column | Type | Notes |
|--------|------|-------|
| `org_id` | TEXT | PK component; org scoping |
| `cluster_uuid` | UUID | Latest cluster for the container |
| `namespace` | TEXT | PK component |
| `workload` | TEXT | PK component |
| `container_name` | TEXT | PK component |
| `workload_type` | TEXT | Denormalized from latest recommendation row |
| `last_reported` | TIMESTAMPTZ | Latest `updated_at` from active recommendations |
| `resolved_tags` | JSONB | Default `{}`; reserved for Koku tag sync |

**Primary key:** `(org_id, namespace, workload, container_name)`

**Indexes:**

- `idx_ock_org_sorted` — `(org_id, namespace, workload, container_name)` for list pagination
- `idx_ock_org_cluster` — `(org_id, cluster_uuid)` for cluster-scoped filters
- `idx_ock_tags` — GIN on `resolved_tags` for future tag containment queries

### Refresh triggers

| Event | Function |
|-------|----------|
| After recommendation write (ingestion) | [`RefreshOrgContainerKeysTx`](../../internal/model/org_container_keys.go) in [`WriteRecommendations`](../../internal/engine/recommend_all.go) |
| After adoption mark | [`RefreshOrgContainerKeys`](../../internal/model/org_container_keys.go) in [`MarkAdopted`](../../internal/engine/adoption.go) |

Refresh upserts active keys from `recommendation_sets WHERE stale = false` and deletes
keys with no remaining active rows. `resolved_tags` is preserved on upsert (not overwritten
by refresh) until Koku pushes an update via the tag sync API.

### Tag filtering

Tag sync is implemented via `POST /api/cost-management/v1/internal/tags/sync` (gated by
`ROS_TAGS_ENABLED`, authenticated with a Kubernetes ServiceAccount bearer token). Sync
freshness is exposed at `GET /api/cost-management/v1/internal/tags/status?org_id=<org_id>`.
Koku pushes namespace-level tags into `org_container_keys.resolved_tags`; the container list applies
`?filter[tag:key]=value1,value2` (Koku syntax) or legacy `?tag=key:value` on step 1 using
the GIN index.

**Filter by tag value** — GIN-indexed containment:

```sql
SELECT * FROM org_container_keys
WHERE org_id = $1
  AND resolved_tags @> '{"environment": "production"}';
```

**Filter by tag key (any value)** — key existence:

```sql
SELECT * FROM org_container_keys
WHERE org_id = $1
  AND resolved_tags ? 'environment';
```

**Group by tag key** — aggregate on the key table (one row per container):

```sql
SELECT resolved_tags->>'environment' AS env, COUNT(*)
FROM org_container_keys
WHERE org_id = $1
GROUP BY 1;
```

List API adds tag predicates to step 1 (key pagination) before joining
`recommendation_sets` for term/engine detail. `?group_by=tag:<key>` grouping is
planned for a follow-up release.

Implementation: [`internal/model/tag_filters.go`](../../internal/model/tag_filters.go),
[`internal/tags/sync.go`](../../internal/tags/sync.go).

Auth details and planned mTLS upgrade: [tag-sync-auth.md](tag-sync-auth.md).
Tag lifecycle and full-replace semantics: [tag-sync.md](tag-sync.md).

---

## Priority Recommendations

Audit action items from the 2026 EXPLAIN pass and follow-up fixes:

| Item | Priority | Status |
|------|----------|--------|
| Remove `rh_accounts` join for org scoping (node util, GPU triples) | P0 | **DONE** |
| BH enrichment: filter digest query by page container keys | P0 | **DONE** |
| Keyset pagination for container list | P0 | **DONE** |
| Partial indexes (keyset, savings, snapshot, node util — migrations 000078–000080) | P1 | **DONE** |
| Materialized `org_container_keys` table (eliminate DISTINCT) | P2 | **DONE** |
| Remaining `rh_accounts` offenders (quality, namespace list, history) | P0 | Open |
| GPU triple fresh-node materialization | P2 | Open |
| Fleet savings materialized summary | P2 | Open |
| Koku tag sync → `org_container_keys.resolved_tags` | P2 | **DONE** (push API + list filter; Koku Celery sender) |

---

## When to Add Indexes vs Rewrite Queries

| Symptom in EXPLAIN | Fix | Priority |
|--------------------|-----|----------|
| Seq scan on column that has an index | Query rewrite — wrong join path prevents index use | **P0** (free) |
| Index scan but expensive sort | Add index matching `ORDER BY` / keyset columns | **P1** (cheap migration) |
| Heap fetches dominate (`Buffers: shared hit` low vs reads) | Add `INCLUDE` columns for covering index | **P1** |
| Filter on non-indexed predicate (`stale`, `term`, `engine`) | Add partial index matching `WHERE` clause | **P1** |
| `DISTINCT` over millions of rows | Materialized key table (`org_container_keys`) | **P2 — DONE** |
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
3. **P2 — Architecture** (materialized views, denormalization, key tables) — container keys **done**; fleet savings and GPU fresh-node tables remain

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
- [ ] No `COUNT(DISTINCT ...)` on hot path — use pre-computed stats or `org_container_keys`
- [ ] Container list paginates `org_container_keys`, not `SELECT DISTINCT` on `recommendation_sets`
- [ ] BH/GPU enrichment queries filter by page container keys, not whole clusters
- [ ] Keyset pagination preferred over OFFSET for user-facing lists
- [ ] Ran explain-audit script or manual `EXPLAIN ANALYZE` at org-large scale

---

## See Also

- [Configuration Reference](configuration.md) — connection pool tuning
- [Monitoring](monitoring.md) — API latency metrics
- [Native Engine Performance](../native-engine-performance.md) — ingestion-side optimizations
