CREATE TABLE IF NOT EXISTS quota_recommendation_history (
    id BIGSERIAL PRIMARY KEY,
    org_id TEXT NOT NULL,
    cluster_uuid UUID NOT NULL,
    namespace TEXT NOT NULL,
    recommendation_type TEXT NOT NULL,
    risk_level TEXT NOT NULL,
    cpu_request_hard_millicores BIGINT,
    cpu_request_used_millicores BIGINT,
    cpu_request_recommended_millicores BIGINT,
    memory_request_hard_bytes BIGINT,
    memory_request_used_bytes BIGINT,
    memory_request_recommended_bytes BIGINT,
    max_utilization_percent DOUBLE PRECISION,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_quota_rec_history_lookup
    ON quota_recommendation_history (org_id, cluster_uuid, namespace, recorded_at DESC);
