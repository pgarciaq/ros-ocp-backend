ALTER TABLE daily_container_digests
  ADD COLUMN IF NOT EXISTS desired_replicas INTEGER,
  ADD COLUMN IF NOT EXISTS available_replicas INTEGER;

ALTER TABLE recommendation_sets
  ADD COLUMN IF NOT EXISTS desired_replicas INTEGER,
  ADD COLUMN IF NOT EXISTS available_replicas INTEGER;
