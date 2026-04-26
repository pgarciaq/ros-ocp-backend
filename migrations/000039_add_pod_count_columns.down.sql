ALTER TABLE daily_container_digests
  DROP COLUMN IF EXISTS pod_count_min,
  DROP COLUMN IF EXISTS pod_count_max,
  DROP COLUMN IF EXISTS pod_count_avg;

ALTER TABLE recommendation_sets
  DROP COLUMN IF EXISTS pod_count_min,
  DROP COLUMN IF EXISTS pod_count_max,
  DROP COLUMN IF EXISTS pod_count_avg;

ALTER TABLE namespace_recommendation_sets
  DROP COLUMN IF EXISTS pod_count_min,
  DROP COLUMN IF EXISTS pod_count_max,
  DROP COLUMN IF EXISTS pod_count_avg;
