DROP INDEX IF EXISTS idx_ns_rec_sets_category;
DROP INDEX IF EXISTS idx_rec_sets_category;

ALTER TABLE namespace_recommendation_sets DROP COLUMN IF EXISTS category_memory;
ALTER TABLE namespace_recommendation_sets DROP COLUMN IF EXISTS category_cpu;
ALTER TABLE namespace_recommendation_sets DROP COLUMN IF EXISTS category;

ALTER TABLE recommendation_sets DROP COLUMN IF EXISTS category_memory;
ALTER TABLE recommendation_sets DROP COLUMN IF EXISTS category_cpu;
ALTER TABLE recommendation_sets DROP COLUMN IF EXISTS category;
