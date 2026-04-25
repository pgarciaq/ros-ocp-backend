-- Allow native namespace recommendation rows to skip the legacy workload_id FK
-- and the legacy JSONB recommendations column.
-- Native rows are identified by term IS NOT NULL; legacy Kruize rows keep term NULL.
ALTER TABLE namespace_recommendation_sets
  ALTER COLUMN workload_id DROP NOT NULL;

ALTER TABLE namespace_recommendation_sets
  ALTER COLUMN recommendations DROP NOT NULL;
