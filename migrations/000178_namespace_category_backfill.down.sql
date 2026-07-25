-- Revert: set category back to NULL for idle/zombie namespaces.
UPDATE namespace_recommendation_sets SET category = NULL
WHERE category IN ('idle', 'zombie');
