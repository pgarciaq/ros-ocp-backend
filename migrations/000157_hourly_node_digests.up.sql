-- hourly_node_digests: per-hour aggregated node metrics for activity heatmaps.
-- Partitioned by RANGE(report_date) with monthly granularity.
CREATE TABLE IF NOT EXISTS hourly_node_digests (
    org_id TEXT NOT NULL,
    cluster_uuid UUID NOT NULL,
    node_name TEXT NOT NULL,
    report_date DATE NOT NULL,
    hour SMALLINT NOT NULL CHECK (hour >= 0 AND hour <= 23),
    cpu_usage_p95_mc INTEGER NOT NULL DEFAULT 0,
    mem_usage_p95_kib INTEGER NOT NULL DEFAULT 0,
    sample_count SMALLINT NOT NULL DEFAULT 0,
    max_pod_count INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
) PARTITION BY RANGE (report_date);

CREATE UNIQUE INDEX IF NOT EXISTS idx_hourly_node_digests_unique
    ON hourly_node_digests (org_id, cluster_uuid, node_name, report_date, hour);

CREATE INDEX IF NOT EXISTS idx_hourly_node_digests_retention
    ON hourly_node_digests (report_date);
