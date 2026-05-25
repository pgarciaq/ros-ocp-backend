-- golang-migrate wraps each file in a transaction, so this migration uses plain
-- CREATE INDEX IF NOT EXISTS (not CONCURRENTLY). For large production databases,
-- run the equivalent CREATE INDEX CONCURRENTLY statements from migrations/README.md
-- first; IF NOT EXISTS makes this migration a no-op when indexes already exist.

CREATE INDEX IF NOT EXISTS idx_rs_savings_agg
    ON recommendation_sets (org_id, cluster_uuid)
    INCLUDE (estimated_monthly_savings_usd)
    WHERE stale = false AND term = 'medium' AND engine = 'cost';

CREATE INDEX IF NOT EXISTS idx_rh_org_recorded
    ON recommendation_history (org_id, recorded_at DESC);

CREATE INDEX IF NOT EXISTS idx_ns_org_updated
    ON namespace_recommendation_sets (org_id, updated_at DESC)
    WHERE term IS NOT NULL AND stale = false;
