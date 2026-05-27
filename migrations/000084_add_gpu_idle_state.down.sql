DROP INDEX IF EXISTS idx_recommendation_sets_gpu_idle_state;

ALTER TABLE recommendation_sets
    DROP COLUMN IF EXISTS gpu_estimated_waste_cents,
    DROP COLUMN IF EXISTS gpu_idle_duration_days,
    DROP COLUMN IF EXISTS gpu_idle_since,
    DROP COLUMN IF EXISTS gpu_idle_state;
