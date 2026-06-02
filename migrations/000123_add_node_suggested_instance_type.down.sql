ALTER TABLE node_recommendations
    DROP COLUMN IF EXISTS suggested_instance_type,
    DROP COLUMN IF EXISTS instance_type_reason;
