ALTER TABLE recommendation_sets
    ALTER COLUMN cluster_uuid TYPE TEXT USING cluster_uuid::text;

ALTER TABLE clusters
    ALTER COLUMN cluster_uuid TYPE TEXT USING cluster_uuid::text;
