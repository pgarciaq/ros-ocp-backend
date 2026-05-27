-- golang-migrate wraps each file in a transaction, so this migration uses plain
-- CREATE INDEX IF NOT EXISTS (not CONCURRENTLY). For large production databases,
-- run the equivalent CREATE INDEX CONCURRENTLY statements from migrations/README.md
-- first; IF NOT EXISTS makes this migration a no-op when indexes already exist.
--
-- GPU idle/zombie state is stored on recommendation_sets (has_gpu rows); there is
-- no separate gpu_recommendation_sets table.

ALTER TABLE recommendation_sets
    ADD COLUMN IF NOT EXISTS gpu_idle_state TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS gpu_idle_since TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS gpu_idle_duration_days INTEGER,
    ADD COLUMN IF NOT EXISTS gpu_estimated_waste_cents BIGINT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_recommendation_sets_gpu_idle_state
    ON recommendation_sets (org_id, gpu_idle_state)
    WHERE gpu_idle_state != 'active' AND has_gpu = TRUE;
