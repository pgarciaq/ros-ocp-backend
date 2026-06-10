DROP INDEX IF EXISTS idx_clusters_ingest_hooks_failed;

ALTER TABLE clusters
    DROP COLUMN IF EXISTS ingest_hooks_failed_at,
    DROP COLUMN IF EXISTS ingest_hooks_failed;
