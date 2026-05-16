DELETE FROM node_recommendations WHERE term != 'medium';
ALTER TABLE node_recommendations DROP CONSTRAINT IF EXISTS node_recommendations_pkey;
ALTER TABLE node_recommendations DROP COLUMN IF EXISTS term;
ALTER TABLE node_recommendations ADD PRIMARY KEY (org_id, cluster_uuid, node);
