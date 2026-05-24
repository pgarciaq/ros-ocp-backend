SELECT pg_advisory_xact_lock(7358001);

ALTER TABLE node_recommendations DROP CONSTRAINT IF EXISTS node_recommendations_pkey;
ALTER TABLE node_recommendations ADD COLUMN IF NOT EXISTS engine TEXT NOT NULL DEFAULT 'cost';
ALTER TABLE node_recommendations ADD PRIMARY KEY (org_id, cluster_uuid, node, term, engine);
