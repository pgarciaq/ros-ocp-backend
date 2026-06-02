DROP INDEX IF EXISTS idx_quota_rec_history_resource_lookup;

ALTER TABLE quota_recommendation_history
    DROP COLUMN IF EXISTS utilization_percent,
    DROP COLUMN IF EXISTS current_used,
    DROP COLUMN IF EXISTS current_hard,
    DROP COLUMN IF EXISTS recommended_hard,
    DROP COLUMN IF EXISTS resource;
