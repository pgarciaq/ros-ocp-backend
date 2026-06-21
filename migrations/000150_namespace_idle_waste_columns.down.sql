DROP INDEX IF EXISTS idx_namespace_recommendation_sets_idle_state;

ALTER TABLE namespace_recommendation_sets
    DROP COLUMN IF EXISTS idle_since,
    DROP COLUMN IF EXISTS idle_duration_days,
    DROP COLUMN IF EXISTS estimated_waste_cents;
