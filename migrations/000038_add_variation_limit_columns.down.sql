-- Revert limit variation columns and restore REAL type for request variation.

-- recommendation_sets
ALTER TABLE recommendation_sets
  DROP COLUMN IF EXISTS variation_cpu_limit_pct,
  DROP COLUMN IF EXISTS variation_memory_limit_pct;

ALTER TABLE recommendation_sets
  ALTER COLUMN variation_cpu_request_pct    TYPE REAL,
  ALTER COLUMN variation_memory_request_pct TYPE REAL;

-- namespace_recommendation_sets
ALTER TABLE namespace_recommendation_sets
  DROP COLUMN IF EXISTS variation_cpu_limit_pct,
  DROP COLUMN IF EXISTS variation_memory_limit_pct;

ALTER TABLE namespace_recommendation_sets
  ALTER COLUMN variation_cpu_request_pct    TYPE REAL,
  ALTER COLUMN variation_memory_request_pct TYPE REAL;

-- historical_namespace_recommendation_sets
ALTER TABLE historical_namespace_recommendation_sets
  DROP COLUMN IF EXISTS variation_cpu_limit_pct,
  DROP COLUMN IF EXISTS variation_memory_limit_pct;

ALTER TABLE historical_namespace_recommendation_sets
  ALTER COLUMN variation_cpu_request_pct    TYPE REAL,
  ALTER COLUMN variation_memory_request_pct TYPE REAL;
