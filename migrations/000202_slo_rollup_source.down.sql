ALTER TABLE hosted_api_bucket_rollups DROP CONSTRAINT IF EXISTS hosted_api_bucket_rollups_pkey;
ALTER TABLE hosted_api_bucket_rollups ADD PRIMARY KEY (org_id, cluster_uuid, hc_cluster_id, window_start, window_end, verb_group, le);
ALTER TABLE hosted_api_bucket_rollups DROP COLUMN IF EXISTS source;
