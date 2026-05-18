-- golang-migrate wraps each file in a transaction, so we use plain
-- CREATE INDEX (not CONCURRENTLY).  The tables are small in practice;
-- for very large deployments, apply these indexes manually with
-- CONCURRENTLY outside the migration runner.

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
