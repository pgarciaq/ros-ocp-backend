-- #644 SLO rollup store (implements #624): bounded per-HC histogram bucket
-- rollups + backend-derived worker pressure. Two normalized tables, HC x
-- window keyed. Retention governed by ROS_HISTORY_RETENTION_DAYS (90d),
-- swept with history (see internal/engine/retention.go historyRetainedTables
-- + slo plugin RetentionProvider).
-- plugin: slo
CREATE TABLE IF NOT EXISTS hosted_api_bucket_rollups (
    window_start  TIMESTAMPTZ NOT NULL,
    window_end    TIMESTAMPTZ NOT NULL,
    org_id        TEXT NOT NULL,
    cluster_uuid  UUID NOT NULL,
    hc_cluster_id TEXT NOT NULL,
    verb_group    TEXT NOT NULL CHECK (verb_group IN ('mutating', 'read', 'other')),
    le            DOUBLE PRECISION NOT NULL,
    bucket_count  BIGINT NOT NULL CHECK (bucket_count >= 0),
    collected_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (org_id, cluster_uuid, hc_cluster_id, window_start, window_end, verb_group, le)
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
        part_name   := 'hosted_api_bucket_rollups_' || to_char(month_start, 'YYYYMM');
        IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = part_name) THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF hosted_api_bucket_rollups FOR VALUES FROM (%L) TO (%L)',
                part_name, month_start, month_end
            );
        END IF;
    END LOOP;
END $$;

-- Backend-derived worker pressure (REVISED from #624 close draft per #644
-- canonical contract): no operator CSV. Derived from node digests
-- (codes 12 NODE_OVERCOMMITTED / 74 NODE_POD_SCHEDULING_LIMIT + freshness
-- gate). Coverage-explicit per-HC window; absence/staleness never healthy.
-- plugin: slo
CREATE TABLE IF NOT EXISTS hosted_worker_pressure (
    window_start    TIMESTAMPTZ NOT NULL,
    window_end      TIMESTAMPTZ NOT NULL,
    org_id          TEXT NOT NULL,
    cluster_uuid    UUID NOT NULL,
    hc_cluster_id   TEXT NOT NULL,
    pressured       BOOLEAN NOT NULL,
    coverage_pct    REAL NOT NULL CHECK (coverage_pct >= 0 AND coverage_pct <= 100),
    signals_present TEXT[] NOT NULL DEFAULT '{}',
    observed_at     TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (org_id, cluster_uuid, hc_cluster_id, window_start, window_end)
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
        part_name   := 'hosted_worker_pressure_' || to_char(month_start, 'YYYYMM');
        IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = part_name) THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF hosted_worker_pressure FOR VALUES FROM (%L) TO (%L)',
                part_name, month_start, month_end
            );
        END IF;
    END LOOP;
END $$;
