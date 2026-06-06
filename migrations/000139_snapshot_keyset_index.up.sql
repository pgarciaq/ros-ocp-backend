CREATE INDEX IF NOT EXISTS idx_snapshot_recs_keyset_page
    ON snapshot_recommendation_sets (org_id, age_days DESC, cluster_uuid, namespace, snapshot_name);
