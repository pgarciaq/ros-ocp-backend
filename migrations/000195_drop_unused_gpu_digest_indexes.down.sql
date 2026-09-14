-- Down reverses 000195 (recreate the pre-000187 GPU digest interval indexes).
CREATE INDEX IF NOT EXISTS idx_ros_gpu_digest_cluster_interval
    ON gpu_container_digests (cluster_uuid, interval_start);
CREATE INDEX IF NOT EXISTS idx_gpu_digest_cluster_interval_node
    ON gpu_container_digests (cluster_uuid, interval_start DESC, node_name);
