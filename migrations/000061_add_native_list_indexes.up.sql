-- golang-migrate wraps each file in a transaction, so this migration uses plain
-- CREATE INDEX IF NOT EXISTS (not CONCURRENTLY). For large production databases,
-- run the equivalent CREATE INDEX CONCURRENTLY statements from migrations/README.md
-- first; IF NOT EXISTS makes this migration a no-op when indexes already exist.

CREATE INDEX IF NOT EXISTS idx_ros_rs_org_cluster_stale_updated_at
    ON recommendation_sets (org_id, cluster_uuid, stale, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_ros_rs_org_workload_type_namespace
    ON recommendation_sets (org_id, workload_type, namespace);

CREATE INDEX IF NOT EXISTS idx_ros_ns_org_cluster_stale_updated_at
    ON namespace_recommendation_sets (org_id, cluster_uuid, stale, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_ros_nr_org_cluster_node_term
    ON node_recommendations (org_id, cluster_uuid, node, term);

CREATE INDEX IF NOT EXISTS idx_ros_gpu_digest_cluster_interval
    ON gpu_container_digests (cluster_uuid, interval_start);
