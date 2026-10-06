-- #393 thin W5 (webhook digest): bounded per-cluster admission-webhook
-- rollup store. Cluster x window x webhook x le keyed cumulative snapshots;
-- the correlator derives p99 + rejected rates at read. Retention governed by
-- ROS_HISTORY_RETENTION_DAYS (90d), swept with history (see
-- internal/engine/retention.go historyRetainedTables).
-- plugin: none (store-only; no dedicated plugin — swept with history)
CREATE TABLE IF NOT EXISTS hosted_api_tax_rollups (
    window_start   TIMESTAMPTZ NOT NULL,
    window_end     TIMESTAMPTZ NOT NULL,
    org_id         TEXT NOT NULL,
    cluster_uuid   UUID NOT NULL,
    webhook_name   TEXT NOT NULL,
    le             DOUBLE PRECISION NOT NULL,
    bucket_count   BIGINT NOT NULL CHECK (bucket_count >= 0),
    total_count    BIGINT CHECK (total_count IS NULL OR total_count >= 0),
    rejected_count BIGINT CHECK (rejected_count IS NULL OR rejected_count >= 0),
    collected_at   TIMESTAMPTZ NOT NULL,
    source         TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (org_id, cluster_uuid, webhook_name, window_start, window_end, le, source)
) PARTITION BY RANGE (window_start);

DO $$
DECLARE
    month_start DATE;
    month_end   DATE;
    part_name   TEXT;
BEGIN
    FOR i IN 0..2 LOOP
        month_start := date_trunc('month', CURRENT_DATE) + (i || ' months')::interval;
        month_end   := month_start + '1 month'::interval;
        part_name   := 'hosted_api_tax_rollups_' || to_char(month_start, 'YYYYMM');
        IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = part_name) THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF hosted_api_tax_rollups FOR VALUES FROM (%L) TO (%L)',
                part_name, month_start, month_end
            );
        END IF;
    END LOOP;
END $$;
