-- Add limit variation columns and convert all variation columns from REAL to INTEGER.
-- The native engine now computes variation as rounded integer percentages.

-- recommendation_sets: add limit variation columns
ALTER TABLE recommendation_sets
  ADD COLUMN IF NOT EXISTS variation_cpu_limit_pct    INTEGER,
  ADD COLUMN IF NOT EXISTS variation_memory_limit_pct INTEGER;

-- recommendation_sets: convert existing request variation from REAL to INTEGER
ALTER TABLE recommendation_sets
  ALTER COLUMN variation_cpu_request_pct    TYPE INTEGER USING ROUND(variation_cpu_request_pct)::INTEGER,
  ALTER COLUMN variation_memory_request_pct TYPE INTEGER USING ROUND(variation_memory_request_pct)::INTEGER;

-- namespace_recommendation_sets: add limit variation columns
ALTER TABLE namespace_recommendation_sets
  ADD COLUMN IF NOT EXISTS variation_cpu_limit_pct    INTEGER,
  ADD COLUMN IF NOT EXISTS variation_memory_limit_pct INTEGER;

-- namespace_recommendation_sets: convert existing request variation from REAL to INTEGER
ALTER TABLE namespace_recommendation_sets
  ALTER COLUMN variation_cpu_request_pct    TYPE INTEGER USING ROUND(variation_cpu_request_pct)::INTEGER,
  ALTER COLUMN variation_memory_request_pct TYPE INTEGER USING ROUND(variation_memory_request_pct)::INTEGER;

-- historical_namespace_recommendation_sets: add limit variation columns
ALTER TABLE historical_namespace_recommendation_sets
  ADD COLUMN IF NOT EXISTS variation_cpu_limit_pct    INTEGER,
  ADD COLUMN IF NOT EXISTS variation_memory_limit_pct INTEGER;

-- historical_namespace_recommendation_sets: convert existing request variation from REAL to INTEGER
ALTER TABLE historical_namespace_recommendation_sets
  ALTER COLUMN variation_cpu_request_pct    TYPE INTEGER USING ROUND(variation_cpu_request_pct)::INTEGER,
  ALTER COLUMN variation_memory_request_pct TYPE INTEGER USING ROUND(variation_memory_request_pct)::INTEGER;
