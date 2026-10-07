-- #673 NodePool inventory store (Child A of #660): per-HC pool-state
-- snapshots keyed hourly; the backend rule (Child B) interprets
-- scaled-to-0 (spec AND status zero) at read. Counts stored verbatim,
-- never pre-filtered. Retention governed by ROS_HISTORY_RETENTION_DAYS
-- (90d), swept with history (see internal/engine/retention.go
-- historyRetainedTables).
-- plugin: none (store-only; no dedicated plugin — swept with history)
CREATE TABLE IF NOT EXISTS hosted_nodepool_rollups (
    window_start   TIMESTAMPTZ NOT NULL,
    window_end     TIMESTAMPTZ NOT NULL,
    org_id         TEXT NOT NULL,
    cluster_uuid   UUID NOT NULL,
    hc_cluster_id  TEXT NOT NULL,
    pool_name      TEXT NOT NULL,
    spec_replicas  BIGINT NOT NULL CHECK (spec_replicas >= 0),
    status_replicas BIGINT NOT NULL CHECK (status_replicas >= 0),
    autoscaling    TEXT NOT NULL DEFAULT '',
    collected_at   TIMESTAMPTZ NOT NULL,
    source         TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (org_id, cluster_uuid, hc_cluster_id, pool_name, window_start, window_end, source)
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
        part_name   := 'hosted_nodepool_rollups_' || to_char(month_start, 'YYYYMM');
        IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = part_name) THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF hosted_nodepool_rollups FOR VALUES FROM (%L) TO (%L)',
                part_name, month_start, month_end
            );
        END IF;
    END LOOP;
END $$;
