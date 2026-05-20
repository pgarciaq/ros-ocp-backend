-- Add has_gpu boolean column to recommendation_sets.
-- This enables SQL-level filtering for GPU presence, fixing pagination
-- correctness when ?has_gpu=true/false is used. Previously, GPU filtering
-- was applied post-pagination, producing incomplete pages and wrong totals.
ALTER TABLE recommendation_sets ADD COLUMN IF NOT EXISTS has_gpu BOOLEAN NOT NULL DEFAULT FALSE;

-- Partial index for efficient filtering (only indexes TRUE rows, which are sparse).
-- Not CONCURRENTLY because fresh installs run inside a transaction.
CREATE INDEX IF NOT EXISTS idx_recommendation_sets_has_gpu
    ON recommendation_sets (org_id, cluster_uuid, namespace, workload, container_name)
    WHERE has_gpu = TRUE;
