DELETE FROM notification_code_definitions WHERE code IN (60, 61, 62, 63);

ALTER TABLE vm_recommendations DROP COLUMN IF EXISTS numa_oversized;
ALTER TABLE vm_recommendations DROP COLUMN IF EXISTS has_shared_storage;
ALTER TABLE vm_recommendations DROP COLUMN IF EXISTS is_redundant_placement;
