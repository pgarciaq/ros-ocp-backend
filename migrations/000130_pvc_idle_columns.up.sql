ALTER TABLE pvc_recommendation_sets
    ADD COLUMN IF NOT EXISTS idle_since TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS idle_duration_days INTEGER;
