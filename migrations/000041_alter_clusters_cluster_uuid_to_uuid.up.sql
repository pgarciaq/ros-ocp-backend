-- Align cluster_uuid columns to UUID type across all tables.
-- Existing TEXT values are valid UUID strings, so the cast is safe.

ALTER TABLE clusters
    ALTER COLUMN cluster_uuid TYPE UUID USING cluster_uuid::uuid;

ALTER TABLE recommendation_sets
    ALTER COLUMN cluster_uuid TYPE UUID USING cluster_uuid::uuid;
