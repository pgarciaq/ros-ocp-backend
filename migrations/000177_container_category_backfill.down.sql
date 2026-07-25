-- Revert: set category back to NULL for idle/zombie containers.
UPDATE recommendation_sets SET category = NULL
WHERE category IN ('idle', 'zombie');
