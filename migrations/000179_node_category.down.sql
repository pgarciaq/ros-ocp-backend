-- Revert: re-create boolean columns, backfill from category, then drop category.

-- Step 1: Re-create the boolean columns
ALTER TABLE node_recommendations ADD COLUMN IF NOT EXISTS is_underutilized BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE node_recommendations ADD COLUMN IF NOT EXISTS is_overcommitted BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE node_recommendation_history ADD COLUMN IF NOT EXISTS is_underutilized BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE node_recommendation_history ADD COLUMN IF NOT EXISTS is_overcommitted BOOLEAN NOT NULL DEFAULT false;

-- Step 2: Backfill booleans from category
UPDATE node_recommendations SET
    is_underutilized = (category = 'underutilized'),
    is_overcommitted = (category = 'overcommitted');

UPDATE node_recommendation_history SET
    is_underutilized = (category = 'underutilized'),
    is_overcommitted = (category = 'overcommitted');

-- Step 3: Drop category column and index
DROP INDEX IF EXISTS idx_node_recommendations_org_category;

ALTER TABLE node_recommendations DROP COLUMN IF EXISTS category;
ALTER TABLE node_recommendation_history DROP COLUMN IF EXISTS category;
