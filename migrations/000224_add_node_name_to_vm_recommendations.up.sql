-- #926: filter[node] on the VM list 503s because node_name was never
-- carried into vm_recommendations (the digests always had it — the rec
-- struct dropped it). Add the column, backfill from the latest digest
-- per VM (the same "latest node" semantics the recommender's NUMA
-- resolution uses), and the existing filter SQL starts working.
ALTER TABLE vm_recommendations ADD COLUMN node_name TEXT NOT NULL DEFAULT '';

-- Backfill: the current node per VM from the newest digest. VMs without
-- digests keep '' (no match under filter[node] — correct: unknown node).
UPDATE vm_recommendations vr
SET node_name = d.node_name
FROM (
    SELECT DISTINCT ON (org_id, cluster_uuid, vm_name)
        org_id, cluster_uuid, vm_name, node_name
    FROM daily_vm_digests
    WHERE node_name <> ''
    ORDER BY org_id, cluster_uuid, vm_name, bucket_date DESC
) d
WHERE vr.org_id = d.org_id
  AND vr.cluster_uuid = d.cluster_uuid
  AND vr.vm_name = d.vm_name;
