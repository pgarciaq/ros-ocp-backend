# Query Performance

> **Last verified:** 2026-08-05

Design principles and methodology for keeping ROS-OCP Backend API queries fast
at fleet scale (200k+ containers per organization).

This page summarizes lessons from a PostgreSQL `EXPLAIN ANALYZE` audit of hot
query paths. For the full audit script and reproduction steps, see the
[explain-audit tool](https://github.com/pgarciaq/ros-ocp-backend/tree/{{ git_branch }}/scripts/explain-audit) in the repository.

**Last updated:** 2026-05-25

---

## Why Query Performance Matters

Each organization can have hundreds of thousands of container recommendations.
A single API list request touches `recommendation_sets`, which stores **six rows
per container** (three time terms × two engines). At 200k containers that is
1.2 million rows — small mistakes in join paths or missing indexes turn
sub-millisecond queries into multi-second timeouts.

---

## The `rh_accounts` Join Anti-Pattern

### Problem

Recommendation tables carry a denormalized `org_id` column. If a query still
joins through `clusters → rh_accounts` to resolve the organization, PostgreSQL
cannot use org-scoped indexes. This forces nested-loop joins through the FK
chain, sequential scans, and disk-backed sorts.

At 200k rows this pattern was **~5,000× slower** than filtering `org_id`
directly.

### Rule

**Always filter on the denormalized `org_id` column directly.**

Use the `clusters` join **only** for:

- RBAC cluster-level access control (`cluster_uuid IN (...)`)
- Returning cluster metadata (alias, source_id)

**Never** use `clusters → rh_accounts` for org scoping when `org_id` is on the
table being queried.

### Detection

Run `EXPLAIN ANALYZE` and look for:

- `Nested Loop` through `clusters` → `rh_accounts` when the query needs org-level data
- `Seq Scan` on tables that have `org_id` indexed
- `Sort` with `external merge` on disk

---

## Index Design Principles

1. **Partial indexes are king** — Most queries filter `WHERE stale = false`
   and/or `WHERE term = 'medium' AND engine = 'cost'`. Partial indexes matching
   these filters are dramatically smaller and faster than full-table indexes.

2. **`INCLUDE` columns for covering indexes** — Savings aggregation benefits
   from `INCLUDE (estimated_savings_cents)` so PostgreSQL can satisfy the
   query from the index without heap fetches.

3. **Leading column = `org_id`** — Virtually every API query starts with org
   scoping. `org_id` should be the leading column on all list and aggregation
   indexes.

4. **Keyset pagination index must match `ORDER BY` exactly** — The container
   list sorts by `(namespace, workload, container_name)`. The supporting index
   on `org_container_keys` uses the same column order after `org_id`.

!!! tip "Production deployments"
    On large databases, create indexes with `CREATE INDEX CONCURRENTLY` before
    running migrations. See [`migrations/README.md`](../migrations/README.md)
    for pre-migration steps.

---

## Pagination Architecture — `org_container_keys`

For the full API pagination contract (keyset `after` vs offset, endpoint matrix, client
guidance), see **[API Pagination](pagination.md)**.

The container list API uses a **2-step query** instead of `SELECT DISTINCT` over
`recommendation_sets`:

1. **Page keys** — Query `org_container_keys` (one row per active container)
   with keyset or offset pagination. Index: `idx_ock_org_sorted`.
2. **Fetch detail** — Join `recommendation_sets` for containers on the page only;
   apply `term`, `engine`, and other detail filters here (at most 6 rows × page limit).

The `org_container_keys` table and `org_recommendation_stats` counts are refreshed
once at the end of each recommendation reconcile cycle (not per 500-container write
batch), and immediately after adoption marks. Keys and counts may be briefly stale
while a multi-batch ingest is in progress; they are current before the next API
query after reconciliation completes.

### Why this matters

At 200k containers (1.2M recommendation rows), runtime `DISTINCT` required
sorting the entire org partition — ~1.2 s for term/engine filters and deep pages.
The key table reduces list latency to **under 5 ms at any page depth**.

### Schema (summary)

| Column | Purpose |
|--------|---------|
| `org_id`, `namespace`, `workload`, `container_name` | Primary key; pagination sort order |
| `cluster_uuid`, `workload_type`, `last_reported` | Denormalized metadata |
| `resolved_tags` (JSONB) | Push-synced tags when `ROS_TAGS_SOURCE=api`; unused for filtering when `source=db` |

### Tag filtering (implemented)

Tag sync writes resolved tags via `POST /api/cost-management/v1/internal/tags/sync`
(`ROS_TAGS_ENABLED`, ServiceAccount bearer token). Sync freshness:
`GET /api/cost-management/v1/internal/tags/status?org_id=<org_id>`.
List API supports Koku `?filter[tag:key]=value1,value2` on `org_container_keys` using `idx_ock_tags`.
See [Tag Filtering](features/tag-filtering.md).

**Auth:** ServiceAccount token via TokenReview API today; **mTLS** planned for on-prem.
See [Tag Filtering](features/tag-filtering.md) and [Configuration — Tag sync](configuration.md#tag-filtering-enabled-tag-keys).

**Group by tag (savings summary):** `GET .../savings-summary?group_by[tag:environment]=*`
groups container savings per tag value via `org_container_keys` (requires
`ROS_TAGS_ENABLED=true`). List endpoints use tag filters on step 1 only — not `group_by`.

Deep EXPLAIN plans and checklists live in the repository under
`docs/operations/query-performance.md` (internal; not published on this site).

---

## Plugin Query Paths (2026 audit + fixes)

The explain-audit script covers GPU MIG, GPU time-slicing, snapshot staleness,
business-hours enrichment, term/engine filters, and node utilization.

| Plugin | Typical latency (org-large) | Status |
|--------|----------------------------|--------|
| Container list (key table + keyset) | **< 5 ms** any page | **Fixed** — `org_container_keys` |
| Term/engine list filters | **< 5 ms** | **Fixed** — detail join only |
| GPU MIG digest fetch | ~21 ms | Healthy |
| GPU time-slicing triple pagination | ~115–170 ms | Borderline — index added in migration 000080 |
| Snapshot list / classify / reconcile | < 3 ms | Healthy — migration 000080 |
| BH digest (single cluster) | ~220 ms | Acceptable |
| BH digest (multi-cluster page) | **< 5 ms** | **Fixed** — page container-key filter |
| Node utilization list | ~2 ms | Healthy — `org_id` rewrite + migration 000080 |

**Recent fixes:**

- **BH enrichment** — Digest queries filter by `(cluster_uuid, namespace, workload,
  container_name)` tuples on the current page, not entire clusters.
- **Container list** — Paginates `org_container_keys` instead of `DISTINCT` on
  `recommendation_sets`.
- **Node utilization / GPU triples** — No longer join through `rh_accounts` when
  `org_id` or a pre-scoped cluster list is available.

---

## Savings Aggregation

The fleet savings summary runs four correlated subqueries (containers, nodes,
PVCs, namespaces), each scanning the full org partition.

**Fix today:** Partial covering indexes on each table (org-scoped, filtered by
term/engine/stale).

**Future:** Materialized per-org fleet summary refreshed during ingestion.

---

## When to Add Indexes vs Rewrite Queries

| Symptom | Fix |
|---------|-----|
| Seq scan on indexed column | Query rewrite — wrong join path preventing index use |
| Index scan but slow sort | Add index with matching `ORDER BY` |
| Heap fetches dominate | Add `INCLUDE` columns for covering index |
| Filter on non-indexed predicate | Add partial index matching `WHERE` clause |
| `DISTINCT` over millions of rows | Materialized key table (`org_container_keys`) — **done** |

Fix in priority order:

1. **P0 — Query rewrites** (free, no migration)
2. **P1 — New indexes** (cheap migration)
3. **P2 — Architecture** (materialized views, denormalization)

---

## Outstanding Items

| Item | Status |
|------|--------|
| `org_container_keys` pagination (eliminate DISTINCT) | **Done** |
| BH enrichment container-key filter | **Done** |
| Keyset pagination + partial indexes (000078–000080) | **Done** |
| Remaining `rh_accounts` join offenders (quality, namespace list, history) | Open |
| GPU triple fresh-node materialization | Open |
| Fleet savings materialized summary | Open |
| Koku tag tables / push sync | `source=db`: join `reporting_ocptags_values`; `source=api`: `resolved_tags` GIN |

---

## Audit Methodology

Use this workflow when adding or changing list/aggregation queries:

1. **Seed realistic volumes** — 200k+ containers for the target org (the audit
   seed script creates `org-small`, `org-medium`, and `org-large` datasets).

2. **Run EXPLAIN** — `EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)` on each hot path.

3. **Look for red flags:**
   - Sequential scans on large tables
   - Sort with external merge (disk spill)
   - Nested loops through FK chains for org scoping
   - Large gap between estimated and actual row counts

4. **Fix in priority order** — rewrites first, then indexes, then architecture.

---

## Reproducing the Audit Locally

```bash
# Start local PostgreSQL
podman run -d --name ros-explain-db -p 25432:5432 \
  -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=ros_explain postgres:16

# Apply migrations
cd ~/dev/koku/ros-ocp-backend
DB_HOST=localhost DB_PORT=25432 DB_NAME=ros_explain \
DB_USER=postgres DB_PASSWORD=postgres DB_SSL=disable \
go run . db migrate up

# Run seed + EXPLAIN audit
PGPASSWORD=postgres go run ./scripts/explain-audit/ \
  -db-host localhost -db-port 25432 -db-name ros_explain
```

The script seeds three org sizes, runs EXPLAIN on every hot query path, and
prints a report with timing, scan types, and recommendations.

| Flag | Purpose |
|------|---------|
| `-seed-only` | Load data without running EXPLAIN |
| `-skip-seed` | Re-run EXPLAIN on existing data |

---

## Related Documentation

- [Configuration](configuration.md) — database pool and performance env vars
- [Monitoring](monitoring.md) — API latency and error metrics
- [Migrations README](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/migrations/README.md) — concurrent index procedures
- Repository `docs/operations/query-performance.md` — full EXPLAIN plans and checklists (internal)
