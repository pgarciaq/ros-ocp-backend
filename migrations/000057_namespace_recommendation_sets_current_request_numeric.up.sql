-- Alter namespace current request columns to fixed-precision numeric.
-- cpu_request_current stores cores; memory_request_current stores bytes.
-- NOTE: Renumbered from 000025 to resolve duplicate. Idempotent (ALTER TYPE to same type is a no-op).
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'namespace_recommendation_sets' AND column_name = 'cpu_request_current') THEN
        ALTER TABLE namespace_recommendation_sets
            ALTER COLUMN cpu_request_current TYPE NUMERIC(10, 4),
            ALTER COLUMN memory_request_current TYPE NUMERIC(20, 4);
    END IF;
END $$;

