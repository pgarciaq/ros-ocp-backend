-- Persist storage and pods capacity freed on namespace quota recommendations (parity with CRQ API).
ALTER TABLE quota_recommendation_sets
    ADD COLUMN IF NOT EXISTS storage_freed_bytes BIGINT,
    ADD COLUMN IF NOT EXISTS pods_freed BIGINT;
