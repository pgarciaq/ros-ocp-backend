ALTER TABLE quota_recommendation_sets
    DROP COLUMN IF EXISTS pods_freed,
    DROP COLUMN IF EXISTS storage_freed_bytes;
