DROP INDEX IF EXISTS idx_clusters_analytics_incomplete;

ALTER TABLE clusters
    DROP COLUMN IF EXISTS analytics_incomplete_at,
    DROP COLUMN IF EXISTS analytics_incomplete;
