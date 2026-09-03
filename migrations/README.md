# Database migrations

Apply migrations via `./rosocp db migrate up` (see project docs).

## CI migration lint

New migrations are checked by [`scripts/lint-migrations.sh`](../scripts/lint-migrations.sh) for non-`CONCURRENTLY` indexes on large tables. When the linter fails, follow [`docs/operations/large-table-migrations.md`](../docs/operations/large-table-migrations.md) and use [`deploy/migrations/concurrent-index-job.yaml`](../deploy/migrations/concurrent-index-job.yaml).

## Index conventions

**Prefer `CREATE INDEX CONCURRENTLY`** for new indexes on large tables so deployments do not block writes during index builds.

PostgreSQL does not allow `CONCURRENTLY` inside a transaction block. `golang-migrate` wraps each migration file in a transaction, so migrations checked into this repo often use plain `CREATE INDEX IF NOT EXISTS` instead. That is correct for small databases and CI; for **very large** production tables, apply concurrent indexes **before** running the matching numbered migration—the `IF NOT EXISTS` clauses then make the migration a no-op.

Existing migrations that already ran cannot be rewritten retroactively.

### Migration 000045 (gpu_container_digests unique index)

Migration 000045 drops and recreates the `gpu_container_digests_natural_key` unique index to include `gpu_model_name`. On populated tables this blocks writes during the index build. For **large** deployments, apply this **before** running the migration:

```sql
-- Drop old index (non-blocking; it's just metadata removal)
DROP INDEX IF EXISTS gpu_container_digests_natural_key;

-- Build new index without blocking writes
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS gpu_container_digests_natural_key
    ON gpu_container_digests (cluster_uuid, namespace, workload, container_name, gpu_model_name, interval_start);
```

Then run `./rosocp db migrate up`; migration 000045's `IF NOT EXISTS` makes it a no-op.

Migrations **000058–000060** alter tables/functions and do not add secondary indexes; no `CONCURRENTLY` changes were applied there.

### Migration 000061 (native list indexes)

Indexes target `recommendation_sets`, `namespace_recommendation_sets`, `node_recommendations`, and `gpu_container_digests`, which can grow to millions of rows in SaaS. To avoid long write locks, run the following **as a pre-migration manual step** against the production database (adjust schema/database name as needed; each statement commits separately):

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ros_rs_org_cluster_stale_updated_at
    ON recommendation_sets (org_id, cluster_uuid, stale, updated_at DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ros_rs_org_workload_type_namespace
    ON recommendation_sets (org_id, workload_type, namespace);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ros_ns_org_cluster_stale_updated_at
    ON namespace_recommendation_sets (org_id, cluster_uuid, stale, updated_at DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ros_nr_org_cluster_node_term
    ON node_recommendations (org_id, cluster_uuid, node, term);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ros_gpu_digest_cluster_interval
    ON gpu_container_digests (cluster_uuid, interval_start);
```

Then run `./rosocp db migrate up` as usual; migration `000061` will skip creating indexes that already exist.

### Migration 000079 (EXPLAIN audit query indexes)

Indexes target savings aggregation, history time-ordered lists, and namespace list queries. For **large** deployments, run the following **as a pre-migration manual step**:

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_rs_savings_agg
    ON recommendation_sets (org_id, cluster_uuid)
    INCLUDE (estimated_savings_cents)
    WHERE stale = false AND term = 'medium' AND engine = 'cost';

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_rh_org_recorded
    ON recommendation_history (org_id, recorded_at DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ns_org_updated
    ON namespace_recommendation_sets (org_id, updated_at DESC)
    WHERE term IS NOT NULL AND stale = false;
```

Then run `./rosocp db migrate up`; migration `000079` will skip creating indexes that already exist.

### Migration 000080 (plugin query indexes)

Indexes for GPU time-slicing, snapshot list/classify, and node utilization paths. For **large** deployments, run as a pre-migration manual step:

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_gpu_digest_cluster_interval_node
    ON gpu_container_digests (cluster_uuid, interval_start DESC, node_name);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_snapshot_recs_org_age
    ON snapshot_recommendation_sets (org_id, age_days DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_snapshot_inventory_org_cluster_ingested
    ON snapshot_inventory (org_id, cluster_uuid, ingested_at DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_nr_org_cluster_node
    ON node_recommendations (org_id, cluster_uuid, node);
```

Then run `./rosocp db migrate up`; migration `000080` will skip creating indexes that already exist.

### Migration 000186 (business-hours cluster digest reads)

Indexes for cluster-wide node and GPU digest loads filtered by `schedule_type`
plus a date/start range (issues #514 / #515). Does **not** replace the GPU
indexes from `000061` / `000080`. For **large** deployments, run as a
pre-migration manual step (`gpu_container_digests` is on the large-table lint
list; `daily_node_digests` is partitioned and can be large on SaaS fleets):

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_daily_node_digests_cluster_sched_date
    ON daily_node_digests (org_id, cluster_uuid, schedule_type, bucket_date, node);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_gpu_container_digests_cluster_sched_start
    ON gpu_container_digests (cluster_uuid, schedule_type, interval_start);
```

Then run `./rosocp db migrate up`; migration `000186` will skip creating indexes that already exist.

### Migration 000187 (GPU digest org_id)

Nullable `org_id` on `gpu_container_digests`, backfill from `clusters`/`rh_accounts`,
and covering index for org-scoped GPU BH prune (issue #512 PR-1). Does **not**
`SET NOT NULL` and does **not** change the unique key. Does **not** replace
`idx_gpu_container_digests_cluster_sched_start` from `000186`.

For **large** deployments, create the org covering index as a pre-migration
manual step (`gpu_container_digests` is on the large-table lint list):

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_gpu_container_digests_org_cluster_sched_start
    ON gpu_container_digests (org_id, cluster_uuid, schedule_type, interval_start);
```

Then run `./rosocp db migrate up`; migration `000187` will skip creating the
index if it already exists. The `ADD COLUMN` + backfill still run in the
migration transaction.

### Migration 000188 (GPU digest org_id NOT NULL)

`SET NOT NULL` on `gpu_container_digests.org_id` (issue #512 PR-2). Re-runs the
000187 backfill, then adds `CHECK (org_id IS NOT NULL) NOT VALID`, `VALIDATE
CONSTRAINT`, `ALTER COLUMN org_id SET NOT NULL`, and drops the CHECK. Does
**not** delete leftover NULLs: if any `org_id IS NULL` remains, migrate **fails**.

`gpu_container_digests` is partitioned and on the large-table lint list.
`VALIDATE CONSTRAINT` scans every partition (`SHARE UPDATE EXCLUSIVE`).
golang-migrate wraps the file in one transaction, so `SET NOT NULL`'s
`ACCESS EXCLUSIVE` lock is held until commit. There is no `CONCURRENTLY`
equivalent. Do not add `000188` to a live database that still has GPU rows
with no `clusters` match.

### Migration 000189 (GPU digest unique includes org_id)

Rebuilds `gpu_container_digests_natural_key` to include `org_id` (issue #512
PR-3):

`(org_id, cluster_uuid, namespace, workload, container_name, gpu_model_name, interval_start, schedule_type)`

Does **not** rewrite GPU SELECTs or housekeeper (PR-4). Does **not** drop
`idx_gpu_container_digests_cluster_sched_start` from `000186`.

For **large** deployments, rebuild the unique index as a pre-migration
manual step (`gpu_container_digests` is on the large-table lint list).
There is a brief window without uniqueness between DROP and CREATE:

```sql
DROP INDEX CONCURRENTLY IF EXISTS gpu_container_digests_natural_key;
CREATE UNIQUE INDEX CONCURRENTLY gpu_container_digests_natural_key
    ON gpu_container_digests (
        org_id, cluster_uuid, namespace, workload, container_name,
        gpu_model_name, interval_start, schedule_type
    );
```

Then run `./rosocp db migrate up`; migration `000189` skips the DROP when
`indexdef` already contains `org_id`, and `CREATE UNIQUE INDEX IF NOT EXISTS`
is a no-op. Down recreates the cluster-scoped unique and **fails** if two
orgs already stored the same cluster-scoped key.

### Migration 000190 (drop 000186 GPU cluster-only index)

Drops `idx_gpu_container_digests_cluster_sched_start` (issue #512 PR-5).
Does **not** delete `migrations/000186_*.sql`. Does **not** drop
`idx_ros_gpu_digest_cluster_interval` (000061) or
`idx_gpu_digest_cluster_interval_node` (000080). Cluster-wide GPU reads
now predicate `org_id` and use `idx_gpu_container_digests_org_cluster_sched_start`.

For **large** deployments, drop the index as a pre-migration manual step
(`gpu_container_digests` is on the large-table lint list):

```sql
DROP INDEX CONCURRENTLY IF EXISTS idx_gpu_container_digests_cluster_sched_start;
```

Then run `./rosocp db migrate up`; migration `000190` is a no-op when the
index is already gone. Down recreates the cluster-only GPU index.

### Migration 000191 (`clusters.org_id`)

Adds nullable `clusters.org_id` (issue #445 slice A). Backfills from
`rh_accounts` via `tenant_id`. Adds `idx_clusters_org_id_uuid
(org_id, cluster_uuid)`. A BEFORE INSERT/UPDATE trigger copies `org_id` from
`rh_accounts` when omitted.

`clusters` is small — not on the large-table lint list. Plain
`CREATE INDEX IF NOT EXISTS` in the migration is enough. Directory SQL still
joins `rh_accounts` until 000192 (slice B). Unique stays
`(tenant_id, source_id, cluster_uuid, cluster_alias)`. Do not drop `tenant_id`.

### Migration 000192 (`clusters.org_id` NOT NULL)

`SET NOT NULL` on `clusters.org_id` (issue #445 slice B). Re-runs the 000191
backfill, then CHECK NOT VALID → VALIDATE → SET NOT NULL (same PG16 pattern
as 000188). Leftover NULLs fail the migration; no DELETE.

Directory SQL and fleet alias joins switch to `c.org_id` in this slice.
`clusters` is small — no `CONCURRENTLY` pre-step.

### Migration 000193 (drop namespace rec `schedule_type`)

Issue #516 Path A. Deletes leftover `business_hours` rows from
`namespace_recommendation_sets` and `historical_namespace_recommendation_sets`,
rebuilds `idx_ns_recs_native_key` / `idx_hist_ns_recs_native_key` without
`schedule_type`, then drops the column. Digest `schedule_type` is unchanged.

These rec tables can be large. On production-sized DBs, `DROP`/`CREATE UNIQUE
INDEX CONCURRENTLY` first, then run `./rosocp db migrate up` (the migration
uses plain `DROP INDEX IF EXISTS` / `CREATE UNIQUE INDEX`). Down re-adds
`schedule_type DEFAULT 'all_hours'` and the old unique; it does not restore
deleted BH rows.

