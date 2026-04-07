-- Phase 1: Recommendation quality metrics (REQ-10.6).
-- Tracks OOM rates, stability, and adoption detection per container.
CREATE TABLE IF NOT EXISTS recommendation_quality (
    measured_at              TIMESTAMPTZ NOT NULL,
    org_id                   TEXT NOT NULL,
    cluster_uuid             UUID NOT NULL,
    namespace                TEXT NOT NULL,
    workload                 TEXT NOT NULL,
    container_name           TEXT NOT NULL,
    oom_events_after_rec     BIGINT,
    stability_pct            REAL,
    adoption_detected        BOOLEAN DEFAULT false,
    recommendation_age_hours BIGINT,
    PRIMARY KEY (org_id, cluster_uuid, namespace, workload, container_name, measured_at)
) PARTITION BY RANGE (measured_at);

DO $$
DECLARE
    month_start DATE;
    month_end   DATE;
    part_name   TEXT;
BEGIN
    FOR i IN 0..2 LOOP
        month_start := date_trunc('month', CURRENT_DATE) + (i || ' months')::interval;
        month_end   := month_start + '1 month'::interval;
        part_name   := 'recommendation_quality_' || to_char(month_start, 'YYYYMM');
        IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = part_name) THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF recommendation_quality FOR VALUES FROM (%L) TO (%L)',
                part_name, month_start, month_end
            );
        END IF;
    END LOOP;
END $$;

-- Phase 1: Recommendation history (REQ-1.12 shadow mode).
-- Stores snapshots of recommendation_sets for trend analysis and
-- comparison between old and new engine outputs.
CREATE TABLE IF NOT EXISTS recommendation_history (
    recorded_at                     TIMESTAMPTZ NOT NULL,
    org_id                          TEXT NOT NULL,
    cluster_uuid                    UUID NOT NULL,
    namespace                       TEXT NOT NULL,
    workload                        TEXT NOT NULL,
    container_name                  TEXT NOT NULL,
    term                            TEXT NOT NULL,
    engine                          TEXT NOT NULL,
    rec_cpu_request_millicores      BIGINT,
    rec_cpu_limit_millicores        BIGINT,
    rec_memory_request_kib          BIGINT,
    rec_memory_limit_kib            BIGINT,
    notification_codes              SMALLINT[],
    confidence_level                REAL,
    estimated_monthly_savings_usd   REAL,
    source_binary                   TEXT,
    PRIMARY KEY (org_id, cluster_uuid, namespace, workload, container_name, term, engine, recorded_at)
) PARTITION BY RANGE (recorded_at);

DO $$
DECLARE
    month_start DATE;
    month_end   DATE;
    part_name   TEXT;
BEGIN
    FOR i IN 0..2 LOOP
        month_start := date_trunc('month', CURRENT_DATE) + (i || ' months')::interval;
        month_end   := month_start + '1 month'::interval;
        part_name   := 'recommendation_history_' || to_char(month_start, 'YYYYMM');
        IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = part_name) THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF recommendation_history FOR VALUES FROM (%L) TO (%L)',
                part_name, month_start, month_end
            );
        END IF;
    END LOOP;
END $$;
