# Query Performance

Design principles and methodology for keeping ROS-OCP Backend API queries fast
at fleet scale (200k+ containers per organization).

This page summarizes lessons from a PostgreSQL `EXPLAIN ANALYZE` audit of hot
query paths. For the full audit script and reproduction steps, see the
[explain-audit tool](https://github.com/redhatinsights/ros-ocp-backend/tree/main/scripts/explain-audit)
in the repository.

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
   from `INCLUDE (estimated_monthly_savings_usd)` so PostgreSQL can satisfy the
   query from the index without heap fetches.

3. **Leading column = `org_id`** — Virtually every API query starts with org
   scoping. `org_id` should be the leading column on all list and aggregation
   indexes.

4. **Keyset pagination index must match `ORDER BY` exactly** — The container
   list sorts by `(namespace, workload, container_name)`. The supporting index
   must use the same column order after `org_id`.

!!! tip "Production deployments"
    On large databases, create indexes with `CREATE INDEX CONCURRENTLY` before
    running migrations. See
    [`migrations/README.md`](https://github.com/redhatinsights/ros-ocp-backend/blob/main/migrations/README.md)
    for pre-migration steps.

---

## DISTINCT Is Expensive at Scale

Container recommendations store six rows per container. The list API needs
distinct containers. At 200k containers (1.2M rows), `DISTINCT` requires
sorting the entire org partition even when an index exists.

### Mitigations

| Technique | Effect |
|-----------|--------|
| **Keyset pagination** | First page ~0.4 ms (index scan + limit) |
| **Pre-computed `org_recommendation_stats`** | Avoids `COUNT(DISTINCT ...)` on every list request |
| **Avoid deep OFFSET** | Page 500 with OFFSET still ~1 s due to sort |

**Future:** A materialized container-key table (one row per container) would
eliminate the DISTINCT sort entirely.

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
| `DISTINCT` over millions of rows | Architectural change (materialized view or key table) |

Fix in priority order:

1. **P0 — Query rewrites** (free, no migration)
2. **P1 — New indexes** (cheap migration)
3. **P2 — Architecture** (materialized views, denormalization)

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
- [Migrations README](https://github.com/redhatinsights/ros-ocp-backend/blob/main/migrations/README.md) — concurrent index procedures
