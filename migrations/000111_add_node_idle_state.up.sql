ALTER TABLE node_recommendations
    ADD COLUMN IF NOT EXISTS idle_state TEXT NOT NULL DEFAULT 'active';

CREATE INDEX IF NOT EXISTS idx_node_recommendations_idle_state
    ON node_recommendations (org_id, idle_state)
    WHERE idle_state != 'active';
