-- golang-migrate wraps each file in a transaction, so this migration uses plain
-- CREATE INDEX IF NOT EXISTS (not CONCURRENTLY). For large production databases,
-- run the equivalent CREATE INDEX CONCURRENTLY statements from migrations/README.md
-- first; IF NOT EXISTS makes this migration a no-op when indexes already exist.

-- GPU time-slicing triple pagination: freshness subquery groups by (cluster, node)
-- and filters interval_start over a lookback window.
CREATE INDEX IF NOT EXISTS idx_gpu_digest_cluster_interval_node
    ON gpu_container_digests (cluster_uuid, interval_start DESC, node_name);

-- Snapshot list API orders by age_days DESC within an org.
CREATE INDEX IF NOT EXISTS idx_snapshot_recs_org_age
    ON snapshot_recommendation_sets (org_id, age_days DESC);

-- Snapshot classify/reconcile reads fresh inventory by org, cluster, and ingested_at.
CREATE INDEX IF NOT EXISTS idx_snapshot_inventory_org_cluster_ingested
    ON snapshot_inventory (org_id, cluster_uuid, ingested_at DESC);

-- Node utilization list scopes by org_id then distinct (cluster, node).
CREATE INDEX IF NOT EXISTS idx_nr_org_cluster_node
    ON node_recommendations (org_id, cluster_uuid, node);
