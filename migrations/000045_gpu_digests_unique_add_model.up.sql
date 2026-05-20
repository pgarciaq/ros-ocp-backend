-- Replace the old unique index (without gpu_model_name) with one that includes it.
-- Only drops the index if it lacks gpu_model_name (i.e., old schema). If the
-- CONCURRENTLY pre-step was already applied (see migrations/README.md), both the
-- conditional DROP and the IF NOT EXISTS CREATE are no-ops.
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_indexes
        WHERE indexname = 'gpu_container_digests_natural_key'
          AND indexdef NOT LIKE '%gpu_model_name%'
    ) THEN
        DROP INDEX gpu_container_digests_natural_key;
    END IF;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS gpu_container_digests_natural_key
    ON gpu_container_digests (cluster_uuid, namespace, workload, container_name, gpu_model_name, interval_start);
