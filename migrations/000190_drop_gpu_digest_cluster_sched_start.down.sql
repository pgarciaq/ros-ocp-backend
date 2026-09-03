CREATE INDEX IF NOT EXISTS idx_gpu_container_digests_cluster_sched_start
    ON gpu_container_digests (
        cluster_uuid,
        schedule_type,
        interval_start
    );
