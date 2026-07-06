-- Add quota_id for O(1) trend lookups.
-- This column stores a deterministic UUID v5 derived from
-- (cluster_uuid, namespace, quota_name), computed by the Go write path.
-- Replaces the O(n) client-side scan previously needed to map a quota ID
-- to its composite key (PERF-01).

ALTER TABLE quota_recommendation_sets
    ADD COLUMN IF NOT EXISTS quota_id TEXT;

CREATE INDEX IF NOT EXISTS idx_quota_recs_quota_id
    ON quota_recommendation_sets (org_id, quota_id);
