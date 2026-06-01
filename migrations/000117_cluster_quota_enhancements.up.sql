ALTER TABLE daily_cluster_quota_digests
    ADD COLUMN IF NOT EXISTS namespaces TEXT,
    ADD COLUMN IF NOT EXISTS storage_request_hard BIGINT,
    ADD COLUMN IF NOT EXISTS storage_request_used BIGINT,
    ADD COLUMN IF NOT EXISTS pods_hard BIGINT,
    ADD COLUMN IF NOT EXISTS pods_used BIGINT,
    ADD COLUMN IF NOT EXISTS object_count_hard BIGINT,
    ADD COLUMN IF NOT EXISTS object_count_used BIGINT;

ALTER TABLE cluster_quota_recommendation_sets
    ADD COLUMN IF NOT EXISTS namespaces TEXT,
    ADD COLUMN IF NOT EXISTS storage_request_hard BIGINT,
    ADD COLUMN IF NOT EXISTS storage_request_used BIGINT,
    ADD COLUMN IF NOT EXISTS storage_request_recommended BIGINT,
    ADD COLUMN IF NOT EXISTS pods_hard BIGINT,
    ADD COLUMN IF NOT EXISTS pods_used BIGINT,
    ADD COLUMN IF NOT EXISTS pods_recommended BIGINT,
    ADD COLUMN IF NOT EXISTS utilization_storage_request_percent INT,
    ADD COLUMN IF NOT EXISTS utilization_pods_percent INT;

CREATE TABLE IF NOT EXISTS cluster_quota_recommendation_history (
    id BIGSERIAL PRIMARY KEY,
    org_id TEXT NOT NULL,
    cluster_uuid UUID NOT NULL,
    cluster_quota_name TEXT NOT NULL,
    resource TEXT NOT NULL,
    recommendation_type TEXT NOT NULL,
    risk_level TEXT NOT NULL,
    recommended_hard BIGINT,
    current_hard BIGINT,
    current_used BIGINT,
    utilization_percent INT,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_crq_rec_history_lookup
    ON cluster_quota_recommendation_history (org_id, cluster_uuid, cluster_quota_name, resource, recorded_at DESC);
