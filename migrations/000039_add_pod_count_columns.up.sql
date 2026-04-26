ALTER TABLE daily_container_digests
  ADD COLUMN IF NOT EXISTS pod_count_min INTEGER,
  ADD COLUMN IF NOT EXISTS pod_count_max INTEGER,
  ADD COLUMN IF NOT EXISTS pod_count_avg INTEGER;

ALTER TABLE recommendation_sets
  ADD COLUMN IF NOT EXISTS pod_count_min INTEGER,
  ADD COLUMN IF NOT EXISTS pod_count_max INTEGER,
  ADD COLUMN IF NOT EXISTS pod_count_avg INTEGER;

ALTER TABLE namespace_recommendation_sets
  ADD COLUMN IF NOT EXISTS pod_count_min INTEGER,
  ADD COLUMN IF NOT EXISTS pod_count_max INTEGER,
  ADD COLUMN IF NOT EXISTS pod_count_avg INTEGER;
