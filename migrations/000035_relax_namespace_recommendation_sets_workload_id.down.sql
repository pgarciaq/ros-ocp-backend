-- Delete native rows before re-applying NOT NULL, since they have NULL workload_id/recommendations.
DELETE FROM namespace_recommendation_sets WHERE term IS NOT NULL;

ALTER TABLE namespace_recommendation_sets
  ALTER COLUMN workload_id SET NOT NULL;

ALTER TABLE namespace_recommendation_sets
  ALTER COLUMN recommendations SET NOT NULL;
