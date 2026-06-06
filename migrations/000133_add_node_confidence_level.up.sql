ALTER TABLE node_recommendations
  ADD COLUMN IF NOT EXISTS confidence_level REAL,
  ADD COLUMN IF NOT EXISTS data_days        INTEGER;
