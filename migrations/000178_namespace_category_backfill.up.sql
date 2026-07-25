-- Backfill category for idle/zombie namespaces that currently have NULL category.
-- Active namespaces already have category set by the engine (undersized/oversized/optimized).
UPDATE namespace_recommendation_sets SET category = CASE
    WHEN idle_state = 'zombie' THEN 'zombie'
    WHEN idle_state = 'idle' THEN 'idle'
    ELSE category
END WHERE category IS NULL AND idle_state IN ('idle', 'zombie');
