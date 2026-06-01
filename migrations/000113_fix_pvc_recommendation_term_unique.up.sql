-- 000064 attempted to drop the auto-generated 4-column unique constraint but used a
-- mismatched constraint name. Drop known variants and ensure the 5-column term key exists.
ALTER TABLE pvc_recommendation_sets
    DROP CONSTRAINT IF EXISTS pvc_recommendation_sets_org_id_cluster_uuid_namespace_persist_key;

ALTER TABLE pvc_recommendation_sets
    DROP CONSTRAINT IF EXISTS pvc_recommendation_sets_org_id_cluster_uuid_namespace_persi_key;

ALTER TABLE pvc_recommendation_sets
    DROP CONSTRAINT IF EXISTS pvc_recommendation_sets_org_cluster_ns_pvc_term_key;

ALTER TABLE pvc_recommendation_sets
    ADD COLUMN IF NOT EXISTS term TEXT NOT NULL DEFAULT 'medium';

ALTER TABLE pvc_recommendation_sets
    ADD CONSTRAINT pvc_recommendation_sets_org_cluster_ns_pvc_term_key
    UNIQUE (org_id, cluster_uuid, namespace, persistentvolumeclaim, term);
