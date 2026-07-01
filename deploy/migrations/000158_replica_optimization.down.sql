ALTER TABLE recommendation_sets
    DROP COLUMN IF EXISTS recommended_replicas,
    DROP COLUMN IF EXISTS replica_confidence,
    DROP COLUMN IF EXISTS replica_explanation;
