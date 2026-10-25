-- W4 fleet-capacity snapshot history (#392, ADR-0334 amendment).
-- One append-only row per (org_id, cluster_uuid, observed_at): raw inputs
-- plus the five native published-sizing scenarios computed at ingest via
-- librobne/hcp, and the five nullable MCE reference gauges. Retention is
-- the core historyRetainedTables sweep at ROS_HISTORY_RETENTION_DAYS
-- (90d) — a plugin RetentionProvider sweep would silently deliver the
-- month cutoff instead (#701).
CREATE TABLE IF NOT EXISTS hcp_capacity_history (
    observed_at TIMESTAMPTZ NOT NULL,
    org_id TEXT NOT NULL,
    cluster_uuid UUID NOT NULL,
    mce_sampled_at TIMESTAMPTZ,
    operator_version TEXT NOT NULL DEFAULT '',
    ocp_version TEXT NOT NULL DEFAULT '',
    mce_version TEXT NOT NULL DEFAULT '',
    native_model_version TEXT NOT NULL,
    collection_interval_minutes BIGINT NOT NULL,
    upload_cycle_minutes BIGINT NOT NULL,
    inventory_complete BOOLEAN NOT NULL,
    current_hc_count BIGINT NOT NULL,
    ready_hcp_count BIGINT NOT NULL,
    average_ready_hcp_qps DOUBLE PRECISION,
    ready_hcp_qps_known BOOLEAN NOT NULL,
    hosted_availability_basis TEXT NOT NULL,
    eligible_worker_cpu_allocatable_millicores BIGINT NOT NULL,
    eligible_worker_memory_allocatable_bytes BIGINT NOT NULL,
    eligible_worker_max_pods BIGINT NOT NULL,
    worker_placement_known BOOLEAN NOT NULL,
    pressure_state TEXT NOT NULL,
    mce_request_max_hcs BIGINT,
    mce_low_qps_max_hcs BIGINT,
    mce_average_qps_max_hcs BIGINT,
    mce_medium_qps_max_hcs BIGINT,
    mce_high_qps_max_hcs BIGINT,
    request_max_hcs BIGINT NOT NULL DEFAULT 0,
    request_limiting_resource TEXT NOT NULL DEFAULT '',
    low_qps_max_hcs BIGINT NOT NULL DEFAULT 0,
    low_qps_limiting_resource TEXT NOT NULL DEFAULT '',
    average_qps_max_hcs BIGINT NOT NULL DEFAULT 0,
    average_qps_limiting_resource TEXT NOT NULL DEFAULT '',
    medium_qps_max_hcs BIGINT NOT NULL DEFAULT 0,
    medium_qps_limiting_resource TEXT NOT NULL DEFAULT '',
    high_qps_max_hcs BIGINT NOT NULL DEFAULT 0,
    high_qps_limiting_resource TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (org_id, cluster_uuid, observed_at)
) PARTITION BY RANGE (observed_at);

CREATE INDEX IF NOT EXISTS idx_hcp_capacity_latest
    ON hcp_capacity_history (org_id, cluster_uuid, observed_at DESC);

DO $$
DECLARE
    month_start DATE;
    month_end DATE;
    part_name TEXT;
BEGIN
    -- Previous + current + next two months: a window ending in the prior
    -- month can be emitted just after midnight on the 1st (#392 plan).
    FOR i IN -1..2 LOOP
        month_start := date_trunc('month', CURRENT_DATE) + (i || ' months')::interval;
        month_end := month_start + '1 month'::interval;
        part_name := 'hcp_capacity_history_' || to_char(month_start, 'YYYYMM');
        IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = part_name) THEN
            EXECUTE format(
                'CREATE TABLE %I PARTITION OF hcp_capacity_history FOR VALUES FROM (%L) TO (%L)',
                part_name, month_start, month_end);
        END IF;
    END LOOP;
END $$;
