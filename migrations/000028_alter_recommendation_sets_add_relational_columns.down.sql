-- Reverse the Phase 2 relational column changes to recommendation_sets.
-- This drops the new PK, re-adds the old UUID PK, and removes denormalized columns.

-- Downgrade policy (migrations/README.md, "Narrowing down-migrations"):
-- refuse over indeterminate survivors — never delete silently. NULL
-- values do not collide (Postgres UNIQUE treats NULLs as distinct), so
-- the guard counts rows with the full key present only.
DO $$
DECLARE collisions BIGINT;
BEGIN
    SELECT COUNT(*) INTO collisions FROM (
        SELECT 1 FROM recommendation_sets
        WHERE workload_id IS NOT NULL AND container_name IS NOT NULL
        GROUP BY workload_id, container_name
        HAVING COUNT(*) > 1
    ) d;
    IF collisions > 0 THEN
        RAISE EXCEPTION 'downgrade 000028 blocked: % workload/container pair(s) have multiple rows; the UQ_Recommendation constraint cannot be restored over them. Deduplicate deliberately (decide which row survives); then force the source version (migrate force 28 — a failed downgrade leaves the database dirty at 27, having executed nothing) and re-run', collisions;
    END IF;
END $$;

ALTER TABLE recommendation_sets DROP CONSTRAINT IF EXISTS recommendation_sets_pkey;

-- Restore legacy NOT NULL constraints.
ALTER TABLE recommendation_sets ALTER COLUMN monitoring_start_time SET NOT NULL;
ALTER TABLE recommendation_sets ALTER COLUMN monitoring_end_time SET NOT NULL;
ALTER TABLE recommendation_sets ALTER COLUMN recommendations SET NOT NULL;

ALTER TABLE recommendation_sets
    DROP COLUMN IF EXISTS term,
    DROP COLUMN IF EXISTS engine,
    DROP COLUMN IF EXISTS current_cpu_request_millicores,
    DROP COLUMN IF EXISTS current_cpu_limit_millicores,
    DROP COLUMN IF EXISTS current_memory_request_kib,
    DROP COLUMN IF EXISTS current_memory_limit_kib,
    DROP COLUMN IF EXISTS rec_cpu_request_millicores,
    DROP COLUMN IF EXISTS rec_cpu_limit_millicores,
    DROP COLUMN IF EXISTS rec_memory_request_kib,
    DROP COLUMN IF EXISTS rec_memory_limit_kib,
    DROP COLUMN IF EXISTS variation_cpu_request_pct,
    DROP COLUMN IF EXISTS variation_memory_request_pct,
    DROP COLUMN IF EXISTS notification_codes,
    DROP COLUMN IF EXISTS confidence_level,
    DROP COLUMN IF EXISTS estimated_monthly_savings_usd,
    DROP COLUMN IF EXISTS recommendation_applied_at,
    DROP COLUMN IF EXISTS stale,
    DROP COLUMN IF EXISTS org_id,
    DROP COLUMN IF EXISTS cluster_uuid,
    DROP COLUMN IF EXISTS namespace,
    DROP COLUMN IF EXISTS workload,
    DROP COLUMN IF EXISTS workload_type;

ALTER TABLE recommendation_sets
    ADD COLUMN id uuid DEFAULT gen_random_uuid() PRIMARY KEY;

ALTER TABLE recommendation_sets
    ADD CONSTRAINT UQ_Recommendation UNIQUE (workload_id, container_name);
