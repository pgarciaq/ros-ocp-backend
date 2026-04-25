-- Add container_id for O(1) detail lookups.
-- This column stores a deterministic UUID v5 derived from
-- (cluster_uuid, namespace, workload, container_name), computed by the Go
-- write path. Replaces the O(n) client-side scan previously needed to
-- map a container ID to its composite key.

ALTER TABLE recommendation_sets
    ADD COLUMN IF NOT EXISTS container_id TEXT;

CREATE INDEX IF NOT EXISTS idx_recommendation_sets_container_id
    ON recommendation_sets (container_id);
