-- Rollback migration 000057: restore namespace current request columns to FLOAT.
ALTER TABLE namespace_recommendation_sets
    ALTER COLUMN cpu_request_current TYPE FLOAT,
    ALTER COLUMN memory_request_current TYPE FLOAT;

