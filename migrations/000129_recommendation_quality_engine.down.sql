ALTER TABLE recommendation_quality DROP CONSTRAINT IF EXISTS recommendation_quality_pkey;

ALTER TABLE recommendation_quality
    ADD PRIMARY KEY (org_id, cluster_uuid, namespace, workload, workload_type, container_name, measured_at);

ALTER TABLE recommendation_quality DROP COLUMN IF EXISTS engine;
