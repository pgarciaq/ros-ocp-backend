DROP INDEX IF EXISTS idx_gpu_container_digests_org_cluster_sched_start;

ALTER TABLE gpu_container_digests
    DROP COLUMN IF EXISTS org_id;
