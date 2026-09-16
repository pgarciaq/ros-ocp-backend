ALTER TABLE clusters DROP CONSTRAINT IF EXISTS chk_clusters_topology;
ALTER TABLE clusters DROP COLUMN IF EXISTS cluster_topology;
