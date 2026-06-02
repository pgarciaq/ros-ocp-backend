DROP INDEX IF EXISTS idx_quota_rec_history_lookup_v2;
ALTER TABLE quota_recommendation_history DROP COLUMN IF EXISTS quota_name;

ALTER TABLE quota_recommendation_sets
    DROP CONSTRAINT IF EXISTS quota_recommendation_sets_org_cluster_namespace_quota_key;

ALTER TABLE quota_recommendation_sets
    ADD CONSTRAINT quota_recommendation_sets_org_id_cluster_uuid_namespace_key
    UNIQUE (org_id, cluster_uuid, namespace);

DROP INDEX IF EXISTS idx_quota_recs_org_namespace_name;

ALTER TABLE quota_recommendation_sets
    DROP COLUMN IF EXISTS utilization_pods_bp,
    DROP COLUMN IF EXISTS utilization_storage_request_bp,
    DROP COLUMN IF EXISTS pods_recommended,
    DROP COLUMN IF EXISTS pods_used,
    DROP COLUMN IF EXISTS pods_hard,
    DROP COLUMN IF EXISTS storage_request_recommended_bytes,
    DROP COLUMN IF EXISTS storage_request_used_bytes,
    DROP COLUMN IF EXISTS storage_request_hard_bytes,
    DROP COLUMN IF EXISTS quota_name;

DROP TABLE IF EXISTS daily_namespace_quota_digests;
