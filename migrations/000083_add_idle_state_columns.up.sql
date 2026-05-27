-- golang-migrate wraps each file in a transaction, so this migration uses plain
-- CREATE INDEX IF NOT EXISTS (not CONCURRENTLY). For large production databases,
-- run the equivalent CREATE INDEX CONCURRENTLY statements from migrations/README.md
-- first; IF NOT EXISTS makes this migration a no-op when indexes already exist.

ALTER TABLE recommendation_sets
    ADD COLUMN IF NOT EXISTS idle_state TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS idle_since TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS idle_duration_days INTEGER,
    ADD COLUMN IF NOT EXISTS estimated_waste_cents BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS peak_cpu_millicores INTEGER,
    ADD COLUMN IF NOT EXISTS peak_memory_bytes BIGINT;

CREATE INDEX IF NOT EXISTS idx_recommendation_sets_idle_state
    ON recommendation_sets (org_id, idle_state)
    WHERE idle_state != 'active';

ALTER TABLE namespace_recommendation_sets
    ADD COLUMN IF NOT EXISTS idle_state TEXT NOT NULL DEFAULT 'active';
