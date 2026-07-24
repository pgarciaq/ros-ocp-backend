-- Restore boolean columns on vm_recommendations.
ALTER TABLE vm_recommendations
    ADD COLUMN IF NOT EXISTS is_idle BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS is_abandoned BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS is_oversized BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS is_power_off_candidate BOOLEAN NOT NULL DEFAULT false;

-- Backfill booleans from category.
UPDATE vm_recommendations SET
    is_idle = (category = 'idle'),
    is_abandoned = (category = 'abandoned'),
    is_oversized = (category = 'oversized'),
    is_power_off_candidate = (category = 'power_off_candidate');

-- Drop category column.
ALTER TABLE vm_recommendations DROP COLUMN IF EXISTS category;

-- Restore indexes.
CREATE INDEX IF NOT EXISTS idx_vm_recommendations_idle ON vm_recommendations(org_id, cluster_uuid) WHERE is_idle = true;
CREATE INDEX IF NOT EXISTS idx_vm_recommendations_oversized ON vm_recommendations(org_id, cluster_uuid) WHERE is_oversized = true;
CREATE INDEX IF NOT EXISTS idx_vm_recommendations_abandoned ON vm_recommendations(org_id, cluster_uuid) WHERE is_abandoned = true;

DROP INDEX IF EXISTS idx_vm_recommendations_category;

-- Restore boolean columns on vm_recommendation_history.
ALTER TABLE vm_recommendation_history
    ADD COLUMN IF NOT EXISTS is_idle BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS is_abandoned BOOLEAN NOT NULL DEFAULT false;

-- Backfill history booleans from category.
UPDATE vm_recommendation_history SET
    is_idle = (category = 'idle'),
    is_abandoned = (category = 'abandoned');

-- Drop category column from history.
ALTER TABLE vm_recommendation_history DROP COLUMN IF EXISTS category;
