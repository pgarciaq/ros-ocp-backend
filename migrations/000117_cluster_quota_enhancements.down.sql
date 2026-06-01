DROP TABLE IF EXISTS cluster_quota_recommendation_history;

ALTER TABLE cluster_quota_recommendation_sets
    DROP COLUMN IF EXISTS utilization_pods_percent,
    DROP COLUMN IF EXISTS utilization_storage_request_percent,
    DROP COLUMN IF EXISTS pods_recommended,
    DROP COLUMN IF EXISTS pods_used,
    DROP COLUMN IF EXISTS pods_hard,
    DROP COLUMN IF EXISTS storage_request_recommended,
    DROP COLUMN IF EXISTS storage_request_used,
    DROP COLUMN IF EXISTS storage_request_hard,
    DROP COLUMN IF EXISTS namespaces;

ALTER TABLE daily_cluster_quota_digests
    DROP COLUMN IF EXISTS object_count_used,
    DROP COLUMN IF EXISTS object_count_hard,
    DROP COLUMN IF EXISTS pods_used,
    DROP COLUMN IF EXISTS pods_hard,
    DROP COLUMN IF EXISTS storage_request_used,
    DROP COLUMN IF EXISTS storage_request_hard,
    DROP COLUMN IF EXISTS namespaces;
