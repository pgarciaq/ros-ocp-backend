DROP INDEX IF EXISTS idx_node_recommendations_idle_state;

ALTER TABLE node_recommendations
    DROP COLUMN IF EXISTS idle_state;
