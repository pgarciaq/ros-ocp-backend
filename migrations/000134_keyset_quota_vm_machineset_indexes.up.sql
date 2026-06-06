CREATE INDEX IF NOT EXISTS idx_quota_recs_keyset_page
    ON quota_recommendation_sets (org_id, namespace, cluster_uuid, quota_name);

CREATE INDEX IF NOT EXISTS idx_crq_recs_keyset_page
    ON cluster_quota_recommendation_sets (org_id, cluster_quota_name, cluster_uuid);

CREATE INDEX IF NOT EXISTS idx_vm_recs_keyset_page
    ON vm_recommendations (org_id, vm_name, namespace, cluster_uuid, term, engine);
