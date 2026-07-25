-- Add category column to node_recommendations and node_recommendation_history,
-- backfill from existing boolean/enum fields, then drop the replaced booleans.

-- Step 1: Add category column with default
ALTER TABLE node_recommendations ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT 'optimized';
ALTER TABLE node_recommendation_history ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT 'optimized';

-- Step 2: Backfill category from existing classification columns.
-- Priority: idle > overcommitted > stranded_cpu > stranded_memory > underutilized > optimized
UPDATE node_recommendations SET category = CASE
    WHEN idle_state IN ('idle', 'zombie') THEN idle_state
    WHEN is_overcommitted THEN 'overcommitted'
    WHEN stranded_resource = 'cpu' THEN 'stranded_cpu'
    WHEN stranded_resource = 'memory' THEN 'stranded_memory'
    WHEN is_underutilized THEN 'underutilized'
    ELSE 'optimized'
END;

UPDATE node_recommendation_history SET category = CASE
    WHEN idle_state IN ('idle', 'zombie') THEN idle_state
    WHEN is_overcommitted THEN 'overcommitted'
    WHEN stranded_resource = 'cpu' THEN 'stranded_cpu'
    WHEN stranded_resource = 'memory' THEN 'stranded_memory'
    WHEN is_underutilized THEN 'underutilized'
    ELSE 'optimized'
END;

-- Step 3: Drop replaced boolean columns
ALTER TABLE node_recommendations DROP COLUMN IF EXISTS is_underutilized;
ALTER TABLE node_recommendations DROP COLUMN IF EXISTS is_overcommitted;

ALTER TABLE node_recommendation_history DROP COLUMN IF EXISTS is_underutilized;
ALTER TABLE node_recommendation_history DROP COLUMN IF EXISTS is_overcommitted;

-- Step 4: Index for category filtering
CREATE INDEX IF NOT EXISTS idx_node_recommendations_org_category ON node_recommendations (org_id, category);
