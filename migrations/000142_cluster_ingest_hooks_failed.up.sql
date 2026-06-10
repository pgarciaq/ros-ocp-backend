ALTER TABLE clusters
    ADD COLUMN IF NOT EXISTS ingest_hooks_failed BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS ingest_hooks_failed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_clusters_ingest_hooks_failed
    ON clusters (ingest_hooks_failed)
    WHERE ingest_hooks_failed = TRUE;
