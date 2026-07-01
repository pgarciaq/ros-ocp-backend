ALTER TABLE recommendation_sets
    ADD COLUMN IF NOT EXISTS recommended_replicas INTEGER,
    ADD COLUMN IF NOT EXISTS replica_confidence TEXT,
    ADD COLUMN IF NOT EXISTS replica_explanation TEXT;
