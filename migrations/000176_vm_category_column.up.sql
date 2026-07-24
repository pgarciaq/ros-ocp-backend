-- Add canonical category column to vm_recommendations.
ALTER TABLE vm_recommendations
    ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT 'optimized';

-- Backfill existing rows from boolean flags.
UPDATE vm_recommendations SET category = CASE
    WHEN is_abandoned THEN 'abandoned'
    WHEN is_power_off_candidate THEN 'power_off_candidate'
    WHEN is_idle THEN 'idle'
    WHEN is_oversized THEN 'oversized'
    WHEN recommended_vcpu > current_vcpu OR recommended_memory_gib > current_memory_gib THEN 'undersized'
    WHEN recommended_vcpu < current_vcpu OR recommended_memory_gib < current_memory_gib THEN 'oversized'
    ELSE 'optimized'
END;

-- Drop superseded boolean columns.
ALTER TABLE vm_recommendations
    DROP COLUMN IF EXISTS is_idle,
    DROP COLUMN IF EXISTS is_abandoned,
    DROP COLUMN IF EXISTS is_oversized,
    DROP COLUMN IF EXISTS is_power_off_candidate;

-- Drop stale partial indexes on the removed columns.
DROP INDEX IF EXISTS idx_vm_recommendations_idle;
DROP INDEX IF EXISTS idx_vm_recommendations_oversized;
DROP INDEX IF EXISTS idx_vm_recommendations_abandoned;

-- New composite index for category-based queries.
CREATE INDEX IF NOT EXISTS idx_vm_recommendations_category
    ON vm_recommendations(org_id, cluster_uuid, category);

-- Also add category column to vm_recommendation_history table.
ALTER TABLE vm_recommendation_history
    ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT 'optimized';

-- Backfill history from boolean flags.
UPDATE vm_recommendation_history SET category = CASE
    WHEN is_abandoned THEN 'abandoned'
    WHEN is_idle THEN 'idle'
    ELSE 'optimized'
END;

-- Drop superseded boolean columns from history table.
ALTER TABLE vm_recommendation_history
    DROP COLUMN IF EXISTS is_idle,
    DROP COLUMN IF EXISTS is_abandoned;
