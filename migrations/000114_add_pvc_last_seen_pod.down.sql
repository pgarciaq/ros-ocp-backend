ALTER TABLE pvc_recommendation_sets
    DROP COLUMN IF EXISTS last_seen_pod;

ALTER TABLE daily_pvc_digests
    DROP COLUMN IF EXISTS last_seen_pod;
