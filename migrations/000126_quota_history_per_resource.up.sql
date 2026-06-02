-- Align namespace quota history with per-resource CRQ history rows.
ALTER TABLE quota_recommendation_history
    ADD COLUMN IF NOT EXISTS resource TEXT,
    ADD COLUMN IF NOT EXISTS recommended_hard BIGINT,
    ADD COLUMN IF NOT EXISTS current_hard BIGINT,
    ADD COLUMN IF NOT EXISTS current_used BIGINT,
    ADD COLUMN IF NOT EXISTS utilization_percent INT;

CREATE INDEX IF NOT EXISTS idx_quota_rec_history_resource_lookup
    ON quota_recommendation_history (org_id, cluster_uuid, namespace, quota_name, resource, recorded_at DESC);
