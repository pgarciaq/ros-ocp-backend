-- #512 PR-3: gpu_container_digests unique key includes org_id.
--
-- New natural key:
--   (org_id, cluster_uuid, namespace, workload, container_name,
--    gpu_model_name, interval_start, schedule_type)
--
-- Prepend org_id only. Do not add workload_type. Partition key
-- interval_start stays in the unique (required on the partitioned parent).
--
-- Does NOT rewrite GPU SELECTs or housekeeper (PR-4). Does NOT drop
-- idx_gpu_container_digests_cluster_sched_start (000186 / PR-5).
--
-- Existing rows cannot collide on the old key, so CREATE UNIQUE INDEX
-- with org_id added succeeds without a merge. After this migration, two
-- tenants may both store GPU days for the same cluster_uuid. Cluster-scoped
-- reads/deletes still mix or wipe both until PR-4 — do not deploy this
-- without PR-4.
--
-- golang-migrate wraps the file in one transaction, so this uses DROP +
-- CREATE UNIQUE INDEX IF NOT EXISTS (not CONCURRENTLY). For large
-- production databases, run the DROP INDEX CONCURRENTLY + CREATE UNIQUE
-- INDEX CONCURRENTLY statements from migrations/README.md first.
-- If the pre-built index already includes org_id, the conditional DROP
-- and IF NOT EXISTS CREATE are no-ops (000045 pattern).
--
-- Advisory: gpu_container_digests is on the large-table lint list.
-- CREATE UNIQUE INDEX does not match the non-unique CREATE INDEX lint.

DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_indexes
        WHERE indexname = 'gpu_container_digests_natural_key'
          AND indexdef NOT LIKE '%org_id%'
    ) THEN
        DROP INDEX gpu_container_digests_natural_key;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS gpu_container_digests_natural_key
    ON gpu_container_digests (
        org_id, cluster_uuid, namespace, workload, container_name,
        gpu_model_name, interval_start, schedule_type
    );
