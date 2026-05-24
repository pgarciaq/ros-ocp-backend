CREATE TABLE IF NOT EXISTS recommendation_thresholds (
    org_id TEXT NOT NULL,
    recommendation_type TEXT NOT NULL,
    thresholds JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, recommendation_type)
);
