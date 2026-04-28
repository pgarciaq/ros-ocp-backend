CREATE UNIQUE INDEX IF NOT EXISTS gpu_container_digests_natural_key
    ON gpu_container_digests (cluster_uuid, namespace, workload, container_name, interval_start);
