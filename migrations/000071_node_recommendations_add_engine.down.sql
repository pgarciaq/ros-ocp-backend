SELECT pg_advisory_xact_lock(7358001);

DELETE FROM node_recommendations WHERE engine = 'performance';

ALTER TABLE node_recommendations DROP CONSTRAINT IF EXISTS node_recommendations_pkey;
ALTER TABLE node_recommendations DROP COLUMN IF EXISTS engine;
ALTER TABLE node_recommendations ADD PRIMARY KEY (org_id, cluster_uuid, node, term);
