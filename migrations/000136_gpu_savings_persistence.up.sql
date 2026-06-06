-- Persist GPU MIG/idle savings at ingestion time (cents), consistent with container savings.
-- Values are refreshed on ingestion and via savings recalculation when cost models change.

ALTER TABLE recommendation_sets
    ADD COLUMN IF NOT EXISTS estimated_gpu_savings_cents BIGINT;
