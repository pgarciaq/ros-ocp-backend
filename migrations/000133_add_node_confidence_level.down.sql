ALTER TABLE node_recommendations
  DROP COLUMN IF EXISTS confidence_level,
  DROP COLUMN IF EXISTS data_days;
