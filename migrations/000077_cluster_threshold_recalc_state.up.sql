CREATE TABLE IF NOT EXISTS cluster_threshold_recalc_state (
    org_id TEXT NOT NULL,
    cluster_uuid UUID NOT NULL,
    recommendation_type TEXT NOT NULL,
    thresholds_hash TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, cluster_uuid, recommendation_type)
);

CREATE INDEX IF NOT EXISTS idx_cluster_threshold_recalc_org_type
    ON cluster_threshold_recalc_state (org_id, recommendation_type);
