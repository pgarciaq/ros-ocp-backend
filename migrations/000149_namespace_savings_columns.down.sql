ALTER TABLE namespace_recommendation_sets
    DROP COLUMN IF EXISTS estimated_savings_cents,
    DROP COLUMN IF EXISTS estimated_cpu_savings_cents,
    DROP COLUMN IF EXISTS estimated_memory_savings_cents;
