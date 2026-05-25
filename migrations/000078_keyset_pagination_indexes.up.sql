-- golang-migrate wraps each file in a transaction, so this migration uses plain
-- CREATE INDEX IF NOT EXISTS (not CONCURRENTLY). For large production databases,
-- run the equivalent CREATE INDEX CONCURRENTLY statements from migrations/README.md
-- first; IF NOT EXISTS makes this migration a no-op when indexes already exist.

CREATE INDEX IF NOT EXISTS idx_rs_keyset_page
    ON recommendation_sets (org_id, namespace, workload, container_name)
    WHERE stale = false;

CREATE TABLE IF NOT EXISTS org_recommendation_stats (
    org_id TEXT PRIMARY KEY,
    container_count BIGINT NOT NULL DEFAULT 0,
    namespace_count BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
