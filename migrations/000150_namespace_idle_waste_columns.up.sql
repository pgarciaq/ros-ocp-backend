ALTER TABLE namespace_recommendation_sets
    ADD COLUMN IF NOT EXISTS idle_since TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS idle_duration_days INTEGER,
    ADD COLUMN IF NOT EXISTS estimated_waste_cents BIGINT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_namespace_recommendation_sets_idle_state
    ON namespace_recommendation_sets (org_id, idle_state)
    WHERE idle_state != 'active';
