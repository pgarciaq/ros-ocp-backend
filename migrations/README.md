# Database migrations

Apply migrations via `./rosocp db migrate up` (see project docs).

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
