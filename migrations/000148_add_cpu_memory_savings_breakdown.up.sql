ALTER TABLE recommendation_sets
    ADD COLUMN IF NOT EXISTS estimated_cpu_savings_cents BIGINT,
    ADD COLUMN IF NOT EXISTS estimated_memory_savings_cents BIGINT;

-- Allow NULL for waste when cost data is unavailable (same semantics as savings).
ALTER TABLE recommendation_sets
    ALTER COLUMN estimated_waste_cents DROP NOT NULL,
    ALTER COLUMN estimated_waste_cents DROP DEFAULT;
