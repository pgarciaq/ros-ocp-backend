-- #634 history hosted distinction rollback.
ALTER TABLE recommendation_history DROP CONSTRAINT IF EXISTS recommendation_history_pkey;
ALTER TABLE recommendation_history ADD PRIMARY KEY (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, recorded_at);
ALTER TABLE recommendation_history DROP COLUMN IF EXISTS hosted_cluster_id;
