-- DROP INDEX CONCURRENTLY cannot run inside a transaction block.

DROP INDEX CONCURRENTLY IF EXISTS idx_ros_gpu_digest_cluster_interval;
DROP INDEX CONCURRENTLY IF EXISTS idx_ros_nr_org_cluster_node_term;
DROP INDEX CONCURRENTLY IF EXISTS idx_ros_ns_org_cluster_stale_updated_at;
DROP INDEX CONCURRENTLY IF EXISTS idx_ros_rs_org_workload_type_namespace;
DROP INDEX CONCURRENTLY IF EXISTS idx_ros_rs_org_cluster_stale_updated_at;
