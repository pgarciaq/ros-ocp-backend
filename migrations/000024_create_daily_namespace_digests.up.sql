-- Phase 2: Daily namespace digest table (populated by Go during CSV ingestion).
-- Same pattern as daily_container_digests but aggregated at the namespace level
-- for namespace quota recommendations (REQ-1.13).
CREATE TABLE IF NOT EXISTS daily_namespace_digests (
    bucket_date             DATE NOT NULL,
    org_id                  TEXT NOT NULL,
    cluster_uuid            UUID NOT NULL,
    namespace               TEXT NOT NULL,
    cpu_request_p50_mc      BIGINT,
    cpu_request_p95_mc      BIGINT,
    cpu_request_p98_mc      BIGINT,
    cpu_usage_p50_mc        BIGINT,
    cpu_usage_p95_mc        BIGINT,
    cpu_usage_max_mc        BIGINT,
    memory_request_p50_kib  BIGINT,
    memory_request_p95_kib  BIGINT,
    memory_usage_p50_kib    BIGINT,
    memory_usage_p95_kib    BIGINT,
    memory_usage_max_kib    BIGINT,
    cpu_usage_mean_mc       BIGINT,
    memory_usage_mean_kib   BIGINT,
    sample_count            BIGINT,
    PRIMARY KEY (org_id, cluster_uuid, namespace, bucket_date)
) PARTITION BY RANGE (bucket_date);

DO $$
DECLARE
    month_start DATE;
    month_end   DATE;
    part_name   TEXT;
BEGIN
    FOR i IN 0..2 LOOP
        month_start := date_trunc('month', CURRENT_DATE) + (i || ' months')::interval;
        month_end   := month_start + '1 month'::interval;
        part_name   := 'daily_namespace_digests_' || to_char(month_start, 'YYYYMM');
        IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = part_name) THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF daily_namespace_digests FOR VALUES FROM (%L) TO (%L)',
                part_name, month_start, month_end
            );
        END IF;
    END LOOP;
END $$;
