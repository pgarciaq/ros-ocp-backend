-- Track quality metrics per recommendation engine (cost vs performance).
ALTER TABLE recommendation_quality
    ADD COLUMN IF NOT EXISTS engine TEXT NOT NULL DEFAULT 'cost';

ALTER TABLE recommendation_quality DROP CONSTRAINT IF EXISTS recommendation_quality_pkey;

ALTER TABLE recommendation_quality
    ADD PRIMARY KEY (org_id, cluster_uuid, namespace, workload, workload_type, container_name, engine, measured_at);
