-- #644 amendment (split by source, live lab 2026-09-30): apiserver_request_duration_seconds_bucket is exposed by multiple jobs (kubernetes, metrics-server, metrics) with different compiled bucket schemas. Per-source rows keep each progression a coherent cumulative histogram; the backend PK gains source. Worker pressure stays per-HC (no source concept).
-- plugin: slo
ALTER TABLE hosted_api_bucket_rollups ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT '';

ALTER TABLE hosted_api_bucket_rollups DROP CONSTRAINT IF EXISTS hosted_api_bucket_rollups_pkey;
ALTER TABLE hosted_api_bucket_rollups ADD PRIMARY KEY (org_id, cluster_uuid, hc_cluster_id, window_start, window_end, verb_group, le, source);
