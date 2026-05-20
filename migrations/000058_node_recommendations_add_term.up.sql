-- Acquire advisory lock 7358001 to serialize with PersistNodeRecommendations.
-- Workers acquire the same lock before writing; this prevents deadlocks without
-- requiring manual worker shutdown. The lock is released at transaction end.
SELECT pg_advisory_xact_lock(7358001);

ALTER TABLE node_recommendations DROP CONSTRAINT IF EXISTS node_recommendations_pkey;
ALTER TABLE node_recommendations ADD COLUMN IF NOT EXISTS term TEXT NOT NULL DEFAULT 'medium';
ALTER TABLE node_recommendations ADD PRIMARY KEY (org_id, cluster_uuid, node, term);
