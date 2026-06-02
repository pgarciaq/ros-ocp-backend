ALTER TABLE pvc_recommendation_sets
    DROP COLUMN IF EXISTS idle_duration_days,
    DROP COLUMN IF EXISTS idle_since;
