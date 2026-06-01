ALTER TABLE cluster_quota_recommendation_sets
    DROP COLUMN IF EXISTS savings_storage_bytes_freed,
    DROP COLUMN IF EXISTS savings_pods_freed;
