ALTER TABLE pvc_recommendation_sets
    DROP CONSTRAINT IF EXISTS pvc_recommendation_sets_org_cluster_ns_pvc_term_key;

ALTER TABLE pvc_recommendation_sets
    DROP COLUMN IF EXISTS term;

ALTER TABLE pvc_recommendation_sets
    DROP CONSTRAINT IF EXISTS pvc_recommendation_sets_org_id_cluster_uuid_namespace_persist_key;

ALTER TABLE pvc_recommendation_sets
    DROP CONSTRAINT IF EXISTS pvc_recommendation_sets_org_id_cluster_uuid_namespace_persi_key;

ALTER TABLE pvc_recommendation_sets
    DROP CONSTRAINT IF EXISTS pvc_recommendation_sets_org_id_cluster_uuid_namespace_persist_k;

ALTER TABLE pvc_recommendation_sets
    ADD CONSTRAINT pvc_recommendation_sets_org_id_cluster_uuid_namespace_persist_key
    UNIQUE (org_id, cluster_uuid, namespace, persistentvolumeclaim);
