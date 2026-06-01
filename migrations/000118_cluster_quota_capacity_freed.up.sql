ALTER TABLE cluster_quota_recommendation_sets
    ADD COLUMN IF NOT EXISTS savings_storage_bytes_freed BIGINT,
    ADD COLUMN IF NOT EXISTS savings_pods_freed BIGINT;
