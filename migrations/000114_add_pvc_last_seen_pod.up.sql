ALTER TABLE daily_pvc_digests
    ADD COLUMN IF NOT EXISTS last_seen_pod TEXT NOT NULL DEFAULT '';

ALTER TABLE pvc_recommendation_sets
    ADD COLUMN IF NOT EXISTS last_seen_pod TEXT NOT NULL DEFAULT '';
