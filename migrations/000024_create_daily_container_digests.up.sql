-- Phase 2: Daily container digest table (populated by Go during CSV ingestion).
-- Go computes exact percentiles via slices.Sort() on ~96 integer values per
-- container per day and upserts into this table. No raw metric readings stored
-- in PostgreSQL — CSVs remain in S3.
CREATE TABLE IF NOT EXISTS daily_container_digests (
    bucket_date         DATE NOT NULL,
    org_id              TEXT NOT NULL,
    cluster_uuid        UUID NOT NULL,
    namespace           TEXT NOT NULL,
    workload            TEXT NOT NULL,
    workload_type       TEXT NOT NULL,
    container_name      TEXT NOT NULL,
    -- Pre-computed percentiles (exact, computed in Go).
    -- All numeric metric columns are BIGINT (int64 end-to-end, see REQ-2.3).
    cpu_request_p50_mc  BIGINT,
    cpu_request_p60_mc  BIGINT,
    cpu_request_p95_mc  BIGINT,
    cpu_request_p98_mc  BIGINT,
    cpu_request_p99_mc  BIGINT,
    cpu_usage_p50_mc    BIGINT,
    cpu_usage_p60_mc    BIGINT,
    cpu_usage_p95_mc    BIGINT,
    cpu_usage_p98_mc    BIGINT,
    cpu_usage_p99_mc    BIGINT,
    cpu_usage_max_mc    BIGINT,
    cpu_throttle_p95_mc BIGINT,
    cpu_throttle_max_mc BIGINT,
    memory_request_p50_kib  BIGINT,
    memory_request_p95_kib  BIGINT,
    memory_usage_p50_kib    BIGINT,
    memory_usage_p95_kib    BIGINT,
    memory_usage_max_kib    BIGINT,
    memory_rss_p95_kib      BIGINT,
    memory_rss_max_kib      BIGINT,
    oom_count_sum       BIGINT DEFAULT 0,
    cpu_usage_mean_mc   BIGINT,
    memory_usage_mean_kib BIGINT,
    sample_count        BIGINT,
    PRIMARY KEY (org_id, cluster_uuid, namespace, workload, workload_type, container_name, bucket_date)
) PARTITION BY RANGE (bucket_date);

-- Create initial monthly partitions (current + next 2 months).
-- Go auto-partition logic will create future partitions at startup.
DO $$
DECLARE
    month_start DATE;
    month_end   DATE;
    part_name   TEXT;
BEGIN
    FOR i IN 0..2 LOOP
        month_start := date_trunc('month', CURRENT_DATE) + (i || ' months')::interval;
        month_end   := month_start + '1 month'::interval;
        part_name   := 'daily_container_digests_' || to_char(month_start, 'YYYYMM');
        IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = part_name) THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF daily_container_digests FOR VALUES FROM (%L) TO (%L)',
                part_name, month_start, month_end
            );
        END IF;
    END LOOP;
END $$;
