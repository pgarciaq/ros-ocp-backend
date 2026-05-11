ALTER TABLE recommendation_sets
  DROP COLUMN IF EXISTS desired_replicas,
  DROP COLUMN IF EXISTS available_replicas;

ALTER TABLE daily_container_digests
  DROP COLUMN IF EXISTS desired_replicas,
  DROP COLUMN IF EXISTS available_replicas;
