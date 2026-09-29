-- W1.2 association persistence (#632) rollback.
DROP INDEX IF EXISTS idx_recommendation_sets_hosted_cluster;
ALTER TABLE recommendation_sets DROP COLUMN IF EXISTS hosted_cluster_id;
DROP INDEX IF EXISTS idx_manifest_hcp_snapshots_lookup;
DROP TABLE IF EXISTS manifest_hcp_snapshots;
