-- Align cluster_uuid columns to UUID type across all tables.
-- Guard: delete any rows with malformed cluster_uuid before casting.
-- Valid UUIDs match the pattern xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx (hex + hyphens).

DELETE FROM recommendation_sets
WHERE cluster_uuid !~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$';

DELETE FROM clusters
WHERE cluster_uuid !~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$';

ALTER TABLE clusters
    ALTER COLUMN cluster_uuid TYPE UUID USING cluster_uuid::uuid;

ALTER TABLE recommendation_sets
    ALTER COLUMN cluster_uuid TYPE UUID USING cluster_uuid::uuid;
