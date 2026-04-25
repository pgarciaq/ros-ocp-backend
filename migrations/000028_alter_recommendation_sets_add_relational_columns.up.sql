-- Phase 2: Add relational columns to recommendation_sets (REQ-2.5).
-- Replaces JSONB recommendations with typed SQL columns. Denormalizes
-- org_id/cluster_uuid/namespace/workload from the workloads/clusters/rh_accounts
-- join chain into this table for the new composite PK.

-- Step 1: Add denormalized identity columns.
ALTER TABLE recommendation_sets
    ADD COLUMN IF NOT EXISTS org_id       TEXT,
    ADD COLUMN IF NOT EXISTS cluster_uuid TEXT,
    ADD COLUMN IF NOT EXISTS namespace    TEXT,
    ADD COLUMN IF NOT EXISTS workload     TEXT,
    ADD COLUMN IF NOT EXISTS workload_type TEXT;

-- Step 2: Backfill denormalized columns from existing data.
UPDATE recommendation_sets rs
SET
    org_id       = w.org_id,
    cluster_uuid = c.cluster_uuid,
    namespace    = w.namespace,
    workload     = w.workload_name,
    workload_type = w.workload_type::TEXT
FROM workloads w
JOIN clusters c ON c.id = w.cluster_id
WHERE rs.workload_id = w.id
  AND rs.org_id IS NULL;

-- Step 3: Add term/engine and all relational recommendation columns.
-- All numeric metric columns are BIGINT (int64 end-to-end, see REQ-2.3).
ALTER TABLE recommendation_sets
    ADD COLUMN IF NOT EXISTS term                          TEXT NOT NULL DEFAULT 'short',
    ADD COLUMN IF NOT EXISTS engine                        TEXT NOT NULL DEFAULT 'cost',
    ADD COLUMN IF NOT EXISTS current_cpu_request_millicores BIGINT,
    ADD COLUMN IF NOT EXISTS current_cpu_limit_millicores   BIGINT,
    ADD COLUMN IF NOT EXISTS current_memory_request_kib     BIGINT,
    ADD COLUMN IF NOT EXISTS current_memory_limit_kib       BIGINT,
    ADD COLUMN IF NOT EXISTS rec_cpu_request_millicores     BIGINT,
    ADD COLUMN IF NOT EXISTS rec_cpu_limit_millicores       BIGINT,
    ADD COLUMN IF NOT EXISTS rec_memory_request_kib         BIGINT,
    ADD COLUMN IF NOT EXISTS rec_memory_limit_kib           BIGINT,
    ADD COLUMN IF NOT EXISTS variation_cpu_request_pct      REAL,
    ADD COLUMN IF NOT EXISTS variation_memory_request_pct   REAL,
    ADD COLUMN IF NOT EXISTS notification_codes             SMALLINT[],
    ADD COLUMN IF NOT EXISTS confidence_level               REAL,
    ADD COLUMN IF NOT EXISTS estimated_monthly_savings_usd  REAL,
    ADD COLUMN IF NOT EXISTS recommendation_applied_at      TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS stale                          BOOLEAN DEFAULT false;

-- Step 4: Drop old constraints that are incompatible with the new PK.
ALTER TABLE recommendation_sets DROP CONSTRAINT IF EXISTS UQ_Recommendation;
ALTER TABLE recommendation_sets DROP CONSTRAINT IF EXISTS recommendation_sets_pkey;

-- Step 5: Remove old UUID PK column (no longer needed).
ALTER TABLE recommendation_sets DROP COLUMN IF EXISTS id;

-- Step 6: Set NOT NULL on denormalized columns (safe after backfill).
-- For fresh databases these will already be NOT NULL from the INSERT path.
-- For existing data, any rows with NULL org_id means orphaned data (workload deleted).
DELETE FROM recommendation_sets WHERE org_id IS NULL;
ALTER TABLE recommendation_sets ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE recommendation_sets ALTER COLUMN cluster_uuid SET NOT NULL;
ALTER TABLE recommendation_sets ALTER COLUMN namespace SET NOT NULL;
ALTER TABLE recommendation_sets ALTER COLUMN workload SET NOT NULL;

-- Step 7: Relax legacy NOT NULL constraints (new Go engine doesn't use these columns).
ALTER TABLE recommendation_sets ALTER COLUMN monitoring_start_time DROP NOT NULL;
ALTER TABLE recommendation_sets ALTER COLUMN monitoring_end_time DROP NOT NULL;
ALTER TABLE recommendation_sets ALTER COLUMN recommendations DROP NOT NULL;

-- Step 8: Add new composite PK (6 rows per container: 3 terms x 2 engines).
ALTER TABLE recommendation_sets ADD PRIMARY KEY
    (org_id, cluster_uuid, namespace, workload, container_name, term, engine);
