ALTER TABLE snapshot_settings
    ADD COLUMN IF NOT EXISTS inventory_fresh_hours INT NOT NULL DEFAULT 6;
