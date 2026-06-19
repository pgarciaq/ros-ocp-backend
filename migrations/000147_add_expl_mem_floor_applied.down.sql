ALTER TABLE recommendation_sets
    DROP COLUMN IF EXISTS expl_mem_floor_applied;

ALTER TABLE recommendation_history
    DROP COLUMN IF EXISTS expl_mem_floor_applied;

ALTER TABLE namespace_recommendation_sets
    DROP COLUMN IF EXISTS expl_mem_floor_applied;

ALTER TABLE historical_namespace_recommendation_sets
    DROP COLUMN IF EXISTS expl_mem_floor_applied;
