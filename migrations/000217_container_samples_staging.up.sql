-- #758: per-manifest staging for container metric samples. UNLOGGED scratch
-- table — bounded-memory manifest batching: past the staging threshold, raw
-- samples land here instead of partial digest upserts; the manifest finalize
-- computes digests per key from staged + in-memory samples and deletes the
-- rows. Not a source of truth: crashes truncate UNLOGGED tables and the
-- manifest retry re-processes the un-done files from scratch.
CREATE UNLOGGED TABLE IF NOT EXISTS container_metric_samples_staging (
    manifest_id TEXT NOT NULL,
    org_id TEXT NOT NULL,
    cluster_uuid UUID NOT NULL,
    namespace TEXT NOT NULL,
    workload TEXT NOT NULL,
    workload_type TEXT NOT NULL,
    container_name TEXT NOT NULL,
    bucket_date DATE NOT NULL,
    schedule_type TEXT NOT NULL,
    interval_start TIMESTAMPTZ NOT NULL,
    pod TEXT NOT NULL,
    cpu_request_mc BIGINT NOT NULL DEFAULT 0,
    cpu_usage_mc BIGINT NOT NULL DEFAULT 0,
    cpu_throttle_mc BIGINT NOT NULL DEFAULT 0,
    mem_request_kib BIGINT NOT NULL DEFAULT 0,
    mem_usage_kib BIGINT NOT NULL DEFAULT 0,
    mem_rss_kib BIGINT NOT NULL DEFAULT 0,
    oom_count BIGINT NOT NULL DEFAULT 0,
    restart_count BIGINT NOT NULL DEFAULT 0,
    workload_pod_count BIGINT NOT NULL DEFAULT 0,
    desired_replicas BIGINT NOT NULL DEFAULT 0,
    available_replicas BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_container_samples_staging_manifest
    ON container_metric_samples_staging (manifest_id);
