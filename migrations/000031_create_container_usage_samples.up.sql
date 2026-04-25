-- Phase 5: Raw per-sample usage measurements for exact boxplot computation.
-- Stores each 15-minute CSV row's CPU and memory usage values.
-- Boxplots are computed at query time via percentile_cont().
CREATE TABLE IF NOT EXISTS container_usage_samples (
    sample_time     TIMESTAMPTZ NOT NULL,
    org_id          TEXT NOT NULL,
    cluster_uuid    UUID NOT NULL,
    namespace       TEXT NOT NULL,
    workload        TEXT NOT NULL,
    workload_type   TEXT NOT NULL,
    container_name  TEXT NOT NULL,
    cpu_usage_mc    BIGINT NOT NULL,
    mem_usage_kib   BIGINT NOT NULL,
    PRIMARY KEY (org_id, cluster_uuid, namespace, workload, container_name, sample_time)
) PARTITION BY RANGE (sample_time);

DO $$
DECLARE
    month_start DATE;
    month_end   DATE;
    part_name   TEXT;
BEGIN
    FOR i IN 0..2 LOOP
        month_start := date_trunc('month', CURRENT_DATE) + (i || ' months')::interval;
        month_end   := month_start + '1 month'::interval;
        part_name   := 'container_usage_samples_' || to_char(month_start, 'YYYYMM');
        IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = part_name) THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF container_usage_samples FOR VALUES FROM (%L) TO (%L)',
                part_name, month_start, month_end
            );
        END IF;
    END LOOP;
END $$;
