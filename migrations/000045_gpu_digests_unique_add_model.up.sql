DROP INDEX IF EXISTS gpu_container_digests_natural_key;
CREATE UNIQUE INDEX gpu_container_digests_natural_key
    ON gpu_container_digests (cluster_uuid, namespace, workload, container_name, gpu_model_name, interval_start);
