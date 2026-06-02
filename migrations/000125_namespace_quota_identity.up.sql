-- Per-ResourceQuota identity and extended quota resources (storage, pods, object counts).

CREATE TABLE IF NOT EXISTS daily_namespace_quota_digests (
    id BIGSERIAL PRIMARY KEY,
    org_id TEXT NOT NULL,
    cluster_uuid UUID NOT NULL,
    namespace TEXT NOT NULL,
    quota_name TEXT NOT NULL DEFAULT '',
    report_date DATE NOT NULL,
    cpu_request_hard BIGINT,
    cpu_request_used BIGINT,
    cpu_limit_hard BIGINT,
    cpu_limit_used BIGINT,
    memory_request_hard BIGINT,
    memory_request_used BIGINT,
    memory_limit_hard BIGINT,
    memory_limit_used BIGINT,
    storage_request_hard BIGINT,
    storage_request_used BIGINT,
    pods_hard BIGINT,
    pods_used BIGINT,
    object_count_hard BIGINT,
    object_count_used BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, cluster_uuid, namespace, quota_name, report_date)
);

CREATE INDEX IF NOT EXISTS idx_ns_quota_digests_lookup
    ON daily_namespace_quota_digests (org_id, cluster_uuid, namespace, quota_name, report_date DESC);

ALTER TABLE quota_recommendation_sets
    ADD COLUMN IF NOT EXISTS quota_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS storage_request_hard_bytes BIGINT,
    ADD COLUMN IF NOT EXISTS storage_request_used_bytes BIGINT,
    ADD COLUMN IF NOT EXISTS storage_request_recommended_bytes BIGINT,
    ADD COLUMN IF NOT EXISTS pods_hard BIGINT,
    ADD COLUMN IF NOT EXISTS pods_used BIGINT,
    ADD COLUMN IF NOT EXISTS pods_recommended BIGINT,
    ADD COLUMN IF NOT EXISTS utilization_storage_request_bp INT,
    ADD COLUMN IF NOT EXISTS utilization_pods_bp INT;

ALTER TABLE quota_recommendation_sets
    DROP CONSTRAINT IF EXISTS quota_recommendation_sets_org_id_cluster_uuid_namespace_key;

ALTER TABLE quota_recommendation_sets
    ADD CONSTRAINT quota_recommendation_sets_org_cluster_namespace_quota_key
    UNIQUE (org_id, cluster_uuid, namespace, quota_name);

CREATE INDEX IF NOT EXISTS idx_quota_recs_org_namespace_name
    ON quota_recommendation_sets (org_id, namespace, quota_name);

ALTER TABLE quota_recommendation_history
    ADD COLUMN IF NOT EXISTS quota_name TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_quota_rec_history_lookup_v2
    ON quota_recommendation_history (org_id, cluster_uuid, namespace, quota_name, recorded_at DESC);
