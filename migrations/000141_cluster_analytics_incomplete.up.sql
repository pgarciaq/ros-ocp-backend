ALTER TABLE clusters
    ADD COLUMN IF NOT EXISTS analytics_incomplete BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS analytics_incomplete_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_clusters_analytics_incomplete
    ON clusters (analytics_incomplete)
    WHERE analytics_incomplete = TRUE;
