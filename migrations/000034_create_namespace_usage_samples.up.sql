-- Phase 6: Raw per-interval namespace usage measurements for exact boxplot computation.
-- Mirrors container_usage_samples but keyed by namespace (no workload/container).
-- Boxplots computed at query time via percentile_cont().
CREATE TABLE IF NOT EXISTS namespace_usage_samples (
    sample_time     TIMESTAMPTZ NOT NULL,
    org_id          TEXT NOT NULL,
    cluster_uuid    UUID NOT NULL,
    namespace       TEXT NOT NULL,
    cpu_usage_mc    BIGINT NOT NULL,
    mem_usage_kib   BIGINT NOT NULL,
    PRIMARY KEY (org_id, cluster_uuid, namespace, sample_time)
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
        part_name   := 'namespace_usage_samples_' || to_char(month_start, 'YYYYMM');
        IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = part_name) THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF namespace_usage_samples FOR VALUES FROM (%L) TO (%L)',
                part_name, month_start, month_end
            );
        END IF;
    END LOOP;
END $$;
