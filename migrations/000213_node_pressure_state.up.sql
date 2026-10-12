-- Tier B narrowed: worst node-pressure ordinal per node/day for the
-- node-pressure notification and consolidation guard.
ALTER TABLE daily_node_digests ADD COLUMN IF NOT EXISTS pressure_state INT DEFAULT 0;
