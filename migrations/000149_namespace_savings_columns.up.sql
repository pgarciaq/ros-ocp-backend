ALTER TABLE namespace_recommendation_sets
    ADD COLUMN IF NOT EXISTS estimated_savings_cents BIGINT,
    ADD COLUMN IF NOT EXISTS estimated_cpu_savings_cents BIGINT,
    ADD COLUMN IF NOT EXISTS estimated_memory_savings_cents BIGINT;
