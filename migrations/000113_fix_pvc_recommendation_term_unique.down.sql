-- 113-up dropped legacy auto-named 4-column constraints (databases
-- migrated before 000064's amendment) and re-established the 5-column
-- term key 64-up created. The inverse restores that predecessor
-- contract: the term-inclusive key. The 4-column contract is 64-down's
-- to restore in a full rollback — adding it here would contradict the
-- term-inclusive contract on populated data (multi-term PVCs are legal
-- post-64) and fail. Restoring the 5-column key is populated-safe by
-- construction: the data already satisfies it.
ALTER TABLE pvc_recommendation_sets
    DROP CONSTRAINT IF EXISTS pvc_recommendation_sets_org_id_cluster_uuid_namespace_persist_key;

ALTER TABLE pvc_recommendation_sets
    DROP CONSTRAINT IF EXISTS pvc_recommendation_sets_org_id_cluster_uuid_namespace_persi_key;

ALTER TABLE pvc_recommendation_sets
    DROP CONSTRAINT IF EXISTS pvc_recommendation_sets_org_id_cluster_uuid_namespace_persist_k;

ALTER TABLE pvc_recommendation_sets
    DROP CONSTRAINT IF EXISTS pvc_recommendation_sets_org_cluster_ns_pvc_term_key;

ALTER TABLE pvc_recommendation_sets
    ADD CONSTRAINT pvc_recommendation_sets_org_cluster_ns_pvc_term_key
    UNIQUE (org_id, cluster_uuid, namespace, persistentvolumeclaim, term);
