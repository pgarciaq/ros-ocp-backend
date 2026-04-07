-- Reverse the Phase 2 relational column changes to recommendation_sets.
-- This drops the new PK, re-adds the old UUID PK, and removes denormalized columns.

ALTER TABLE recommendation_sets DROP CONSTRAINT IF EXISTS recommendation_sets_pkey;

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
