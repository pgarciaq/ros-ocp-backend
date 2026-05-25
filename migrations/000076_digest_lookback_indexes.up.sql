-- golang-migrate wraps each file in a transaction, so this migration uses plain
-- CREATE INDEX IF NOT EXISTS (not CONCURRENTLY). For large production databases,
-- run the equivalent CREATE INDEX CONCURRENTLY statements from migrations/README.md
-- first; IF NOT EXISTS makes this migration a no-op when indexes already exist.

CREATE INDEX IF NOT EXISTS idx_daily_container_digests_lookback
    ON daily_container_digests (org_id, cluster_uuid, schedule_type, bucket_date);

CREATE INDEX IF NOT EXISTS idx_daily_namespace_digests_lookback
    ON daily_namespace_digests (org_id, cluster_uuid, schedule_type, bucket_date);
