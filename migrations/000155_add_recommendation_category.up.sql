ALTER TABLE recommendation_sets ADD COLUMN IF NOT EXISTS category TEXT;
ALTER TABLE recommendation_sets ADD COLUMN IF NOT EXISTS category_cpu TEXT;
ALTER TABLE recommendation_sets ADD COLUMN IF NOT EXISTS category_memory TEXT;

ALTER TABLE namespace_recommendation_sets ADD COLUMN IF NOT EXISTS category TEXT;
ALTER TABLE namespace_recommendation_sets ADD COLUMN IF NOT EXISTS category_cpu TEXT;
ALTER TABLE namespace_recommendation_sets ADD COLUMN IF NOT EXISTS category_memory TEXT;

CREATE INDEX IF NOT EXISTS idx_rec_sets_category
    ON recommendation_sets (org_id, category) WHERE category IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_ns_rec_sets_category
    ON namespace_recommendation_sets (org_id, category) WHERE category IS NOT NULL;
