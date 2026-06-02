ALTER TABLE node_recommendations
    ADD COLUMN IF NOT EXISTS suggested_instance_type TEXT,
    ADD COLUMN IF NOT EXISTS instance_type_reason TEXT;
