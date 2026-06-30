-- hourly_vm_digests: per-hour aggregated VM metrics for activity heatmaps.
-- Partitioned by RANGE(report_date) with monthly granularity.
CREATE TABLE IF NOT EXISTS hourly_vm_digests (
    org_id TEXT NOT NULL,
    cluster_uuid UUID NOT NULL,
    namespace TEXT NOT NULL,
    vm_name TEXT NOT NULL,
    report_date DATE NOT NULL,
    hour SMALLINT NOT NULL CHECK (hour >= 0 AND hour <= 23),
    cpu_usage_p95_mc INTEGER NOT NULL DEFAULT 0,
    mem_usage_p95_kib INTEGER NOT NULL DEFAULT 0,
    sample_count SMALLINT NOT NULL DEFAULT 0,
    disk_read_iops_p95 INTEGER NOT NULL DEFAULT 0,
    disk_write_iops_p95 INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
) PARTITION BY RANGE (report_date);

CREATE UNIQUE INDEX IF NOT EXISTS idx_hourly_vm_digests_unique
    ON hourly_vm_digests (org_id, cluster_uuid, namespace, vm_name, report_date, hour);

CREATE INDEX IF NOT EXISTS idx_hourly_vm_digests_retention
    ON hourly_vm_digests (report_date);
