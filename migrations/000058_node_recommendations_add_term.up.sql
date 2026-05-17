-- PK rebuild takes an ACCESS EXCLUSIVE lock but node_recommendations is small
-- (one row per node per term per cluster), so this completes in milliseconds.
ALTER TABLE node_recommendations DROP CONSTRAINT IF EXISTS node_recommendations_pkey;
ALTER TABLE node_recommendations ADD COLUMN IF NOT EXISTS term TEXT NOT NULL DEFAULT 'medium';
ALTER TABLE node_recommendations ADD PRIMARY KEY (org_id, cluster_uuid, node, term);
